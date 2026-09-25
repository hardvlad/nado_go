<?php

namespace ProfitBot\Modules\ExternalApi;

use Exception;
use ProfitBot\Modules\Http\Request;
use ProfitBot\Modules\Models\Entities\KaspiOrder;
use ProfitBot\Modules\Models\Entities\KaspiOrderDetail;
use ProfitBot\Modules\Models\Repositories\KaspiOrderDetails;
use ProfitBot\Modules\Models\Repositories\KaspiOrders;
use Throwable;

class KaspiImport
{
    protected Request $request;
    protected string|null $kaspiToken = null;
    protected KaspiAPI $kaspiApi;

    protected int|null $shopID = null;
    protected int|null $userID = null;
    protected string|null $header_MerchantID = null;

    protected bool $alertNewOrders=false;

    protected $merchantId;
    protected $ampCookie;
    protected $mc_session_cookie;
    protected $mc_sid_cookie;

    protected $lkLogin;
    protected $lkPassword;

    protected $waMessage;

    protected $cacheNameTables = [];

    public function __construct(Request $request, string|null $token=null, int|null $shopID=null, int|null $userID=null)
    {
        $this->request = $request;
        $this->kaspiToken = $token;
        $this->shopID = $shopID;
        $this->userID = $userID;
        $data = $this->request->db->select_row_array("select * from UserKaspiShops where ID=? and UserID=?", [ $this->shopID, $this->userID ]);
        $this->header_MerchantID = $data[0]['KaspiMerchantID'] ?? '';
        $this->kaspiApi = new KaspiAPI($request, $this->kaspiToken, $this->shopID, $this->userID,$this->header_MerchantID, $data[0]['DoNotSendMerchantHeader'] ?? 0);

        $this->waMessage = new WhatsappOrderMessage($request, $this->userID, $this->shopID);
        if (isset($data[0]['LKAccessData']))
        {
            $this->lkLogin = $data[0]['LKEmail'];
            $this->lkPassword = $data[0]['LKPassword'];

            $decoded = json_decode($data[0]['LKAccessData'], true);
            if ($data[0]['LKAccessActive'] == 1 && $this->validatePersonalCabinetCreds($decoded))
            {
                $this->merchantId = $decoded['merchantId'];
                $this->ampCookie = $decoded['ampCookie'];
                $this->mc_session_cookie = $decoded['mc_session_cookie'];
                $this->mc_sid_cookie = $decoded['mc_sid_cookie'];
            }
        }
    }

    public function setAlertNewOrders(bool $alertNewOrders): void
    {
        $this->alertNewOrders = $alertNewOrders;
    }

    public function importOrder(string $id, array|null $details=null, $orderHash = null, $order = null): void
    {
        $orderData = null;
        if (!isset($order))
        {
            $order = $this->kaspiApi->getOrderByID($id);

            if (!isset($order['data']))
            {
                return;
            }
            $orderData = $order['data'];
            $orderHash = $this->kaspiApi->lastResponseHash;
        }
        else
            $orderData = $order;

        if (!isset($orderData['id']))
        {
            return;
        }

        $table = 'KaspiOrders';
        $columns = ['ID','LastOrderResponseHash'];
        $wherecolumns = ['ShopID', 'KaspiID'];
        $bindarr = [ $this->shopID, $orderData['id'] ];
        $row = $this->request->db->select_row_redis("SELECT TOP 1 ID, LastOrderResponseHash from KaspiOrders where ShopID=? and KaspiID=? order by ID DESC",
            $table, $columns, $wherecolumns, $bindarr);

        $ID = null;
        if (isset($row))
        {
            [ $ID, $LastOrderResponseHash ] = $row;
            if (isset($LastOrderResponseHash) && $LastOrderResponseHash == $orderHash)
            {
                return;
            }
            else
            {
                $key = $this->request->db->getRedisKey($table, $columns, $wherecolumns, $bindarr);
                $this->request->db->redis->setValue($key, [ $ID, $orderHash ]);
            }
        }

        try
        {
            $insertData = $this->insertOrderToDB($orderData, $orderHash, $ID);
        }
        catch(Throwable $e)
        {
            $this->request->SendMessageToTelegram("Ошибка при импорте заказов для магазина " . $this->shopID . "\n " . $e->getTraceAsString());
            return;
        }

        if (!isset($insertData))
            return;

        [$orderID, $storeID, $lastDetailsHash, $orderHashIsSame, $isUpdate, $isStatusOrStateChanged, $deliveryCostForSeller] = $insertData;

        if (!isset($details))
        {
            $details = $this->kaspiApi->getItemsInOrderByOrderID($id);
            if (isset($details) && $lastDetailsHash == $this->kaspiApi->lastResponseHash)
            {
                if ($this->alertNewOrders && $isStatusOrStateChanged)
                {
                    $this->waMessage->processOrderFromImport($orderID, !$isUpdate);
                }

                return;
            }
            $this->request->db->do("update KaspiOrders set LastDetailsResponseHash=? where ID=?",[ $this->kaspiApi->lastResponseHash, $orderID ]);
        }

        if (!isset($details))
        {
//            print("Error while getting details for OrderID {$id}: " . $this->kaspiApi->getErrorMessage()) . "\n";
            return;
        }

        $this->insertOrderDetailsToDB($orderID,$details, $storeID, $deliveryCostForSeller);

        if ($this->alertNewOrders && $isStatusOrStateChanged)
        {
            $this->waMessage->processOrderFromImport($orderID, !$isUpdate);
        }
    }

    public function insertOrderToDB(array $orderData, $orderResponseHash, $TableOrderID): ?array
    {
        if (!isset($orderData))
        {
            return null;
        }

        $isUpdate = false;
        $isStatusOrStateChanged = false;
        $isWholeOrderStateChanged = false;
        $CurrentOrderStatusID = null;
        $deliveryCostForSeller = null;

        if (isset($TableOrderID))
            $orderDbData = $this->request->db->select_row_array("select * from KaspiOrders where ID=?",[ $TableOrderID ]);

        $StateID = $this->getNameID($orderData['attributes']['state'], 'KaspiOrderStates');
        $StatusID = $this->getNameID($orderData['attributes']['status'], 'KaspiOrderStatuses');
        $OrderStatusID = $this->getOrderStatusID($orderData['attributes']['state'], $orderData['attributes']['status']);
        if (isset($orderDbData[0]['ID']))
        {
            $isUpdate = true;
            $CurrentOrderStatusID = $orderDbData[0]['OrderStatusID'];
            $isWholeOrderStateChanged = ($CurrentOrderStatusID != $OrderStatusID);
            $orderID = (int)$orderDbData[0]['ID'];
        }

        [$customerID, $customerVersionID, $customerAddressID] = $this->saveCustomerToDB($orderData['attributes']['customer'] ?? null,$orderData['attributes']['deliveryAddress'] ?? null);

        $table = new KaspiOrders(new KaspiOrder($this->request->db));
        $item = new KaspiOrder($this->request->db);
        $item->ShopID = $this->shopID;
        $item->KaspiID = $orderData['id'];
        $item->Code = $orderData['attributes']['code'] ?? null;
        $item->TotalPrice = $orderData['attributes']['totalPrice'] ?? null;
        $item->PaymentModeID = $this->getNameID($orderData['attributes']['paymentMode'], 'KaspiPaymentModes');
        $item->PlannedDeliveryDate = msTimeToSQL($orderData['attributes']['plannedDeliveryDate'] ?? null);
        $item->PlannedDeliveryDateTS = msTimeToInt($orderData['attributes']['plannedDeliveryDate'] ?? null);
        $item->CreationDate = msTimeToSQL($orderData['attributes']['creationDate']+5*3600*1000);
        $item->CreationDateTS = msTimeToInt($orderData['attributes']['creationDate']);
        $deliveryCostForSeller = $orderData['attributes']['deliveryCostForSeller'] ?? null;
        $item->DeliveryCostForSeller = $deliveryCostForSeller;
        $item->IsKaspiDelivery = boolToBit($orderData['attributes']['isKaspiDelivery']);
        $item->DeliveryModeID = $this->getNameID($orderData['attributes']['deliveryMode'], 'KaspiDeliveryModes');
        $item->SignatureRequired = boolToBit($orderData['attributes']['signatureRequired']);
        $item->CreditTerm = $orderData['attributes']['creditTerm'] ?? null;
        $item->PreOrder = boolToBit($orderData['attributes']['preOrder']);
        $item->PickupPointID = $this->getNameID($orderData['attributes']['pickupPointId'], 'KaspiPickupPointIDS');
        $item->ApprovedByBankDate = msTimeToSQL($orderData['attributes']['approvedByBankDate']+5*3600*1000);
        $item->ApprovedByBankDateTS = msTimeToInt($orderData['attributes']['approvedByBankDate']);
        $item->StateID = $StateID;
        $item->StatusID = $StatusID;
        $item->OrderStatusID = $OrderStatusID;
        $item->DeliveryCost = $orderData['attributes']['deliveryCost'] ?? null;

        $item->StoreID = $this->getStoreID($this->shopID, $orderData['attributes']['originAddress'] ?? null, $orderData['attributes']['pickupPointId'] ?? null);
        $item->LastOrderResponseHash = $orderResponseHash;

        $item->CustomerID = $customerID;
        $item->CustomerVersionID = $customerVersionID;
        $item->CustomerAddressID = $customerAddressID;

        $item->Assembled = boolToBit($orderData['attributes']['assembled']);
        if ( isset($orderData['attributes']['assembled']) && $orderData['attributes']['assembled'] && (!$isUpdate || !isset($orderDbData[0]['Assembled']) || $orderDbData[0]['Assembled']==0) )
        {
            $item->AssembledDate = date('Y-m-d H:i:s');
        }

        $item->CourierTransmissionPlanningDate = msTimeToSQL($orderData['attributes']['kaspiDelivery']['courierTransmissionPlanningDate'] ?? null);
        $item->CourierTransmissionPlanningDateTS = msTimeToInt($orderData['attributes']['kaspiDelivery']['courierTransmissionPlanningDate'] ?? null);

        $item->CourierTransmissionDate = msTimeToSQL($orderData['attributes']['kaspiDelivery']['courierTransmissionDate'] ?? null);
        $item->CourierTransmissionDateTS = msTimeToInt($orderData['attributes']['kaspiDelivery']['courierTransmissionDate'] ?? null);

        $item->Express = boolToBit($orderData['attributes']['kaspiDelivery']['express'] ?? null);
        $item->ReturnedToWarehouse = boolToBit($orderData['attributes']['kaspiDelivery']['returnedToWarehouse'] ?? null);

        $item->WaybillNumber = $orderData['attributes']['kaspiDelivery']['waybillNumber'] ?? null;
        if ( isset($item->WaybillNumber) && (!$isUpdate || !isset($orderDbData[0]['WaybillNumber'])) )
        {
            $item->WaybillNumberSetDate = date('Y-m-d H:i:s');
        }

        if ($isUpdate)
        {
            $table->editOne($item, [ "ID" => $orderID ]);
        }
        else
        {
            $table->setOne($item);
            $orderID = $this->request->db->GetLastID();
            if ($this->alertNewOrders)
            {
                $this->newOrderInserted($orderID, $orderData);
            }
        }

        if (!$isUpdate || $orderDbData[0]['StateID'] != $StateID)
        {
            $isStatusOrStateChanged = true;
            $this->request->db->do("insert into KaspiOrdersStateHistory (OrderID, StateID) values (?, ?)", [ $orderID, $StateID ]);
        }

        if (!$isUpdate || $orderDbData[0]['StatusID'] != $StatusID)
        {
            $isStatusOrStateChanged = true;
            $this->request->db->do("insert into KaspiOrdersStatusHistory (OrderID, StatusID) values (?, ?)", [ $orderID, $StatusID ]);
        }

        if ($isUpdate && ($orderDbData[0]['StatusID'] != $StatusID || $orderDbData[0]['StateID'] != $StateID))
        {
            $isStatusOrStateChanged = true;
            $this->orderStateOrStatusChanged($orderID, $orderDbData[0]['StatusID'], $StatusID, $orderDbData[0]['StateID'], $StateID);
        }

        if ($isUpdate &&
            (
                ($item->Assembled != $orderDbData[0]['Assembled'])
                ||
                (isset($item->CourierTransmissionDate) && !isset($orderDbData[0]['CourierTransmissionDate']))
            )
        )
        {
            $isStatusOrStateChanged = true;
        }

        if (!$isUpdate || $isWholeOrderStateChanged)
        {
            $this->wholeOrderStatusChanged($orderID, $CurrentOrderStatusID, $OrderStatusID);
        }

        return [ $orderID, $item->StoreID, $orderDbData[0]['LastDetailsResponseHash'] ?? null, false, $isUpdate, $isStatusOrStateChanged, $deliveryCostForSeller ];
    }

    public function newOrderInserted(int $orderID, array $orderData): void
    {
//        $this->request->SendMessageToTelegram("New order {$orderData['id']} with Status {$orderData['attributes']['status']} and State {$orderData['attributes']['state']} in shop {$this->shopID} for total price {$orderData['attributes']['totalPrice']}");
    }

    public function orderStateOrStatusChanged(int $orderID, int $oldStatusID, int $newStatusID, int $oldStateID, int $newStateID): void
    {
//        $this->request->SendMessageToTelegram("Order {$orderID} status changed from {$oldStatusID} to {$newStatusID} or state changed from {$oldStateID} to {$newStateID}");
        if ($oldStatusID != $newStatusID && $newStatusID == 4) // CANCELLED
        {

        }
    }

    public function insertOrderDetailsToDB($orderID, array $details, $storeID, $deliveryCostForSeller): void
    {
        if (!isset($details['data'])) {
            return;
        }

        $table = new KaspiOrderDetails(new KaspiOrderDetail($this->request->db));
        $detailIndex = 0;
        $detailsCount = count($details['data']);
        $countedCost = 0;
        foreach ($details['data'] as $detail)
        {
            $detailIndex++;
            $isUpdate = false;
            $detailDbData = $this->request->db->select_row_array("select * from KaspiOrderDetails a where KaspiID=? and KaspiOrderID=?",[ $detail['id'], $orderID ]);
            if (isset($detailDbData[0]['ID']))
            {
                $isUpdate = true;
                $detailID = (int)$detailDbData[0]['ID'];
                $curDetSebes = $detailDbData[0]['CurrentSebes'];
            }

            $item = new KaspiOrderDetail($this->request->db);
            $detailCost = null;
            if (isset($deliveryCostForSeller))
            {
                if ($detailIndex == $detailsCount)
                    $detailCost = $deliveryCostForSeller - $countedCost;
                else
                {
                    $detailCost = (int)($deliveryCostForSeller / $detailsCount);
                    $countedCost += $detailCost;
                }
            }
            if (!$isUpdate)
                $item->OrderDeliveryCost = $detailCost;

            $item->KaspiOrderID = $orderID;
            $item->KaspiID = $detail['id'];
            [$masterProductID, $masterVersionID, $productID, $productVersionID, $currentSebes, $isSebesFromNal, $parentProductID, $itemTotalQuant] = $this->saveKaspiProduct($detail, $storeID);
            $item->ProductID = $productID;
            $item->ParentProductID = $parentProductID;
//            $item->MasterProductVersionID = $masterVersionID;
//            $item->MerchantProductVersionID = $productVersionID;
            if (!isset($curDetSebes))
                $item->CurrentSebes = $currentSebes;
            $item->UnitTypeID = $this->getNameID($detail['attributes']['unitType'] ?? 'PIECES', 'KaspiUnitTypes');
            $item->OfferCode = $detail['attributes']['offer']['code'] ?? null ;
            $item->OfferName = $detail['attributes']['offer']['name'] ?? null;
            $item->Quant = $detail['attributes']['quantity'] ?? null;
            $item->TotalPrice = $detail['attributes']['totalPrice'] ?? null;
            $item->BasePrice = $detail['attributes']['basePrice'] ?? null;
            $item->ItemWeight = $detail['attributes']['weight'] ?? null;
            $item->DeliveryCost = $detail['attributes']['deliveryCost'] ?? null;

            $catName = $detail['attributes']['category']['title'] ?? null;
            $catCode = $detail['attributes']['category']['code'] ?? null;
            $item->ItemCategoryID = $this->insertKaspiItemCategory($catCode, $catName);

            $catCode = str_replace('Master - ', '', $catCode);

            $data = $this->request->db->select_row_array("select * from KaspiCategories where Name=?",[ $catName ]);
            $item->CategoryID = isset($data[0]['ID']) ? (int)$data[0]['ID'] : null;
//            $item->CategoryCommission = isset($data[0]['CommissionStart']) ? (float)$data[0]['CommissionStart'] : null;

            $data = $this->request->db->select_row_array("select ID,(select TOP 1 CommissionStart from KaspiCategoriesFromMenuCommissionHistory where CategoryID=KaspiCategoriesFromMenu.ID and Date<(select CreationDate from KaspiOrders where ID=?) order by Date DESC) as CommissionStart from KaspiCategoriesFromMenu where Code=?",[ $orderID, $catCode ]);
            $item->MenuCategoryID = isset($data[0]['ID']) ? (int)$data[0]['ID'] : null;
            if (!isset($item->CategoryCommission, $item->MenuCategoryID))
            {
                $docData = $this->request->db->select_row_array("select CommissionWithNDS from KaspiCategoriesDocPercent where Cat5=?",[ $catName ]);
                if (isset($docData[0]['CommissionWithNDS']))
                    $item->CategoryCommission = (float)$docData[0]['CommissionWithNDS'];

                if (!isset($item->CategoryCommission))
                    $item->CategoryCommission = isset($data[0]['CommissionStart']) ? (float)$data[0]['CommissionStart'] : null;
                if (!isset($item->MenuCategoryID))
                    $item->MenuCategoryID = isset($data[0]['ID']) ? (int)$data[0]['ID'] : null;
                if (!isset($item->CategoryCommission,$item->MenuCategoryID))
                {
                    $data = $this->request->db->select_row_array("select ID,(select TOP 1 CommissionStart from KaspiCategoriesFromMenuCommissionHistory where CategoryID=KaspiCategoriesFromMenu.ID and Date<(select CreationDate from KaspiOrders where ID=?) order by Date DESC) as CommissionStart from KaspiCategoriesFromMenu where Code LIKE ? order by Level desc",[ $orderID, $catCode . '%' ]);
                    if (!isset($item->CategoryCommission))
                        $item->CategoryCommission = isset($data[0]['CommissionStart']) ? (float)$data[0]['CommissionStart'] : null;
                    if (!isset($item->MenuCategoryID))
                        $item->MenuCategoryID = isset($data[0]['ID']) ? (int)$data[0]['ID'] : null;

                    if (!isset($item->CategoryCommission,$item->MenuCategoryID))
                    {
                        $data = $this->request->db->select_row_array("select ID,(select TOP 1 CommissionStart from KaspiCategoriesFromMenuCommissionHistory where CategoryID=KaspiCategoriesFromMenu.ID and Date<(select CreationDate from KaspiOrders where ID=?) order by Date DESC) as CommissionStart from KaspiCategoriesFromMenu where Code LIKE ?",[ $orderID, '%' . $catCode . '%' ]);
                        if (!isset($item->CategoryCommission))
                            $item->CategoryCommission = isset($data[0]['CommissionStart']) ? (float)$data[0]['CommissionStart'] : null;
                        if (!isset($item->MenuCategoryID))
                            $item->MenuCategoryID = isset($data[0]['ID']) ? (int)$data[0]['ID'] : null;
                    }
                }
            }

            try
            {
                if ($isUpdate)
                {
                    $table->editOne($item, [ "ID" => $detailID ]);
                }
                else
                {
                    $table->setOne($item);
                    $detailID = $this->request->db->GetLastID();
                }
                $this->insertOrderStoreOperation($isSebesFromNal,$detailID, $productID, $parentProductID, $storeID, $item, $currentSebes, $itemTotalQuant);
            }
            catch (\Exception $exception)
            {
                print_r($exception->getMessage());
            }
        }
    }

    public function insertOrderStoreOperation($isSebesFromNal, $detailID, $productID, $parentProductID, $storeID, $item, $currentSebes, $itemTotalQuant)
    {
        if ( $this->alertNewOrders && $itemTotalQuant >= $item->Quant )
        {
            $existedOperation = $this->request->db->select_row_array("select ID from UserKaspiShopsStoresOperations where KaspiOrderDetailID=? and OperationTypeID=2",[ $detailID ]);

            $this->request->db->do("if not exists (select ID from UserKaspiShopsStoresOperations where KaspiOrderDetailID=? and OperationTypeID=2) insert into UserKaspiShopsStoresOperations (ProductID,StoreID,Quant,Price,Sebes,OperationTypeID,KaspiOrderDetailID,ReasonID,SourceID) values (?, ?, ?, ?, ?, ?, ?,1,1)",
                [ $detailID, $parentProductID ?? $productID, $storeID, $item->Quant, $item->BasePrice, $currentSebes ?? 0, 2, $detailID ]);

            if (!isset($existedOperation[0]['ID']))
            {
                $presentsData = $this->request->db->select_row_array("select * from KaspiMerchantProductsPresents where MerchantProductID=?", [ $productID ]);
                if (isset($presentsData) && count($presentsData) > 0)
                {
                    foreach ($presentsData as $present)
                    {
                        [ $sebes, $totalQuant ] = $this->getSebesFromItemNal($present['PresentMerchantProductID'], $storeID);
                        if (isset($totalQuant) && $totalQuant >= $item->Quant)
                        {
                            $this->request->db->do("if not exists (select ID from UserKaspiShopsStoresOperations where KaspiOrderDetailID=? and ProductID=? and OperationTypeID=2) insert into UserKaspiShopsStoresOperations (ProductID,StoreID,Quant,Price,Sebes,OperationTypeID,KaspiOrderDetailID,ReasonID,SourceID) values (?, ?, ?, ?, ?, ?, ?,1,1)",
                                [ $detailID, $present['PresentMerchantProductID'], $present['PresentMerchantProductID'], $storeID, $item->Quant, 0, $sebes ?? 0, 2, $detailID ]);
                        }
                    }
                }
            }
        }
    }

    public function insertKaspiItemCategory($code, $name): int|null
    {
        $data = $this->request->db->select_row_array("select ID from KaspiOrderItemCategories where Code=?",[ $code ]);
        if (isset($data[0]['ID']))
            return $data[0]['ID'];
        $this->request->db->do("insert into KaspiOrderItemCategories (Name, Code) values (?, ?)",[ $name, $code ]);
        return $this->request->db->GetLastID();
    }

    public function saveKaspiProduct(array $detail, $storeID): array|null
    {
        if (!isset($detail))
            return null;

        $masterProduct = $this->kaspiApi->getMasterProductByID($detail['relationships']['product']['data']['id']);
        if (!isset($masterProduct))
        {
            return null;
        }

        [$masterProductID, $versionID] = $this->insertUpdateMasterProduct($masterProduct['data']['attributes']['code'], $masterProduct['data']['attributes']['name']);
/*
        $data = $this->request->db->select_row_array("select *,(select TOP 1 ID from KaspiMasterProductsVesions where MasterProductID=a.ID order by ID DESC) as VersionID from KaspiMasterProducts a where KaspiID=?",[ $detail['relationships']['product']['data']['id'] ]);
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into KaspiMasterProducts (KaspiID, Name, Code) values (?, ?, ?)",[ $detail['relationships']['product']['data']['id'], $masterProduct['data']['attributes']['name'], $masterProduct['data']['attributes']['code'] ]);
            $masterProductID = $this->request->db->GetLastID();

            $this->request->db->do("insert into KaspiMasterProductsVesions (MasterProductID, KaspiID, Name, Code) values (?, ?, ?, ?)",[ $masterProductID, $detail['relationships']['product']['data']['id'], $masterProduct['data']['attributes']['name'], $masterProduct['data']['attributes']['code'] ]);
            $versionID = $this->request->db->GetLastID();
        }
        else
        {
            $masterProductID = (int)$data[0]['ID'];
            $versionID = (int)$data[0]['VersionID'];
            if ($data[0]['Name'] != $masterProduct['data']['attributes']['name'] || $data[0]['Code'] != $masterProduct['data']['attributes']['code'])
            {
                $this->request->db->do("insert into KaspiMasterProductsVesions (MasterProductID, KaspiID, Name, Code) values (?, ?, ?, ?)",[ $masterProductID, $detail['relationships']['product']['data']['id'], $masterProduct['data']['attributes']['name'], $masterProduct['data']['attributes']['code'] ]);
                $versionID = $this->request->db->GetLastID();
            }
        }
*/
        [$productID, $productVersionID, $currentSebes, $sebesFromNal, $parentProductID, $itemTotalQuant, $wasCreatedNew] = $this->saveKaspiMerchantProduct($masterProductID, $detail, $storeID);
        return [$masterProductID, $versionID, $productID, $productVersionID, $currentSebes, $sebesFromNal, $parentProductID, $itemTotalQuant];
    }

    public function saveKaspiMerchantProduct(int $masterProductID, array $detail, $storeID): ?array
    {
        if (!isset($detail))
            return null;

        return $this->insertUpdateMerchantProduct($masterProductID, $detail['attributes']['offer']['code'], $detail['attributes']['offer']['name'], $storeID);
/*
        $sebesFromNal = false;

        $data = $this->request->db->select_row_array("select *,(select TOP 1 ID from KaspiMerchantProductsVersions where MerchantProductID=a.ID order by ID DESC) as VersionID from KaspiMerchantProducts a where Code=? and UserID=?",[ $detail['attributes']['offer']['code'], $this->userID ]);
        $currentSebes = null;
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into KaspiMerchantProducts (KaspiMasterProductID, Code, Name, UserID) values (?, ?, ?, ?)",[ $masterProductID, $detail['attributes']['offer']['code'], $detail['attributes']['offer']['name'], $this->userID ]);
            $productID = $this->request->db->GetLastID();

            $this->request->db->do("insert into KaspiMerchantProductsVersions (MerchantProductID, Code, Name) values (?, ?, ?)",[ $productID, $detail['attributes']['offer']['code'], $detail['attributes']['offer']['name'] ]);
            $productVersionID = $this->request->db->GetLastID();
        }
        else
        {
            $productID = (int)$data[0]['ID'];
            $productVersionID = (int)$data[0]['VersionID'];
            $currentSebes = $data[0]['CurrentSebes'];
            $sebes = $this->getSebesFromItemNal($productID, $storeID);
            if (isset($sebes))
            {
                $currentSebes = $sebes;
                $sebesFromNal = true;
            }

            if ($data[0]['Name'] != $detail['attributes']['offer']['name'])
            {
                $this->request->db->do("insert into KaspiMerchantProductsVersions (MerchantProductID, Code, Name) values (?, ?, ?)",[ $productID, $detail['attributes']['offer']['code'], $detail['attributes']['offer']['name'] ]);
                $productVersionID = $this->request->db->GetLastID();
            }
        }

        return [$productID, $productVersionID, $currentSebes, $sebesFromNal];
*/
    }

    public function saveCustomerToDB(array|null $customer, array|null $address): array
    {
        if (!isset($customer))
        {
            return [null, null, null];
        }

        $data = $this->request->db->select_row_array("select *,(select TOP 1 ID from KaspiCustomersVersions where CustomerID=a.ID order by ID DESC) as VersionID,(select TOP 1 ID from KaspiCustomersAdresses where CustomerID=a.ID and FormattedAddress=?) as AddressID from KaspiCustomers a where KaspiID=?",
            [ $address['formattedAddress'] ?? '', $customer['id'] ]);
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into KaspiCustomers (KaspiID, Name, Phone, FirstName, LastName) values (?, ?, ?, ?, ?)",
                [ $customer['id'], $customer['name'] ?? '', $customer['cellPhone'] ?? '', $customer['firstName'] ?? '', $customer['lastName'] ?? '' ]
            );

            $id = $this->request->db->GetLastID();

            $this->request->db->do("insert into KaspiCustomersVersions (CustomerID, KaspiID, Name, Phone, FirstName, LastName) values (?, ?, ?, ?, ?, ?)",
                [ $id, $customer['id'], $customer['name'] ?? '', $customer['cellPhone'] ?? '', $customer['firstName'] ?? '', $customer['lastName'] ?? '' ]
            );

            $versionId = $this->request->db->GetLastID();

            $addressId = $this->saveCustomerAddress($address, $id);

            return [$id, $versionId, $addressId];
        }

        $id = (int)$data[0]['ID'];
        $versionId = (int)$data[0]['VersionID'];

        if ($data[0]['Name'] != $customer['name'] || ($data[0]['Phone'] != $customer['cellPhone'] && $customer['cellPhone'] != '+0(000)-000-00-00') || $data[0]['FirstName'] != $customer['firstName'] || $data[0]['LastName'] != $customer['lastName'])
        {
            if ($customer['cellPhone'] != '+0(000)-000-00-00')
            {
                $this->request->db->do("update KaspiCustomers set Name=?, Phone=?, FirstName=?, LastName=? where ID=?", [ $customer['name'] ?? '', $customer['cellPhone'] ?? '', $customer['firstName'] ?? '', $customer['lastName'] ?? '', $id ] );

                $this->request->db->do("insert into KaspiCustomersVersions (CustomerID, KaspiID, Name, Phone, FirstName, LastName) values (?, ?, ?, ?, ?, ?)",
                    [ $id, $customer['id'], $customer['name'] ?? '', $customer['cellPhone'] ?? '', $customer['firstName'] ?? '', $customer['lastName'] ?? '' ]
                );

                $versionId = $this->request->db->GetLastID();
            }
            else
            {
                $this->request->db->do("update KaspiCustomers set Name=?, FirstName=?, LastName=? where ID=?", [ $customer['name'] ?? '', $customer['firstName'] ?? '', $customer['lastName'] ?? '', $id ] );
            }
        }

        if (!isset($data[0]['AddressID']))
        {
            $addressId = $this->saveCustomerAddress($address, $id);
        }
        else
        {
            $addressId = (int)$data[0]['AddressID'];
        }


        return [$id, $versionId, $addressId];
    }

    public function getNameID(string|null $name, string $table): int|null
    {
        if (!isset($name))
        {
            return null;
        }

        if (isset($this->cacheNameTables[$table. '_' . $name]))
        {
            return $this->cacheNameTables[$table. '_' . $name];
        }

        $data = $this->request->db->select_row_array("select ID from {$table} where Name=?",[ $name ]);
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into {$table} (Name) values (?)", [ $name ]);
            $id = $this->request->db->GetLastID();
        }
        else
            $id = (int)$data[0]['ID'];

        $this->cacheNameTables[$table. '_' . $name] = $id;

        return $id;
    }

    public function getIDByKaspiID(string $kaspiID, string $table, int|null $shopID=null): int|null
    {
        $sql = "select ID from {$table} where KaspiID=?";
        $params = [ $kaspiID ];
        if (isset($shopID))
        {
            $sql .= " and ShopID=?";
            $params[] = $shopID;
        }

        $data = $this->request->db->select_row_array($sql, $params);
        if (!isset($data[0]['ID']))
        {
            return null;
        }
        return (int)$data[0]['ID'];
    }

    public function saveCustomerAddress(array|null $address, int|null $id): int|null
    {
        if (!isset($address))
        {
            return null;
        }

        $townID = $this->getNameID($address['town'], 'KaspiTowns');

        $this->request->db->do("insert into KaspiCustomersAdresses (CustomerID, StreetName, StreetNumber, TownID, District, Building, Apartment, FormattedAddress, Latitude, Longitude) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            [$id, $address['streetName'] ?? '', $address['streetNumber'], $townID, $address['district'] ?? '', $address['building'] ?? '', $address['apartment'] ?? '', $address['formattedAddress'],
                $address['latitude'], $address['longitude']
            ]
        );

        return $this->request->db->GetLastID();
    }

    public function importAllOrdersStartingFromDate(string $date): void
    {
//        print ("Started Importing orders from {$date}\n");
        $unixDate = DateToUnix($date);
        while ($unixDate < time() + 86400)
        {
            $days = 3;
            $this->importOrdersInPeriod($unixDate * 1000, ($unixDate + $days * 86400) * 1000);
            $unixDate += $days * 86400;
        }
    }

    public function importShopOrders(): void
    {
//        print ("Started New Importing New orders\n");
        $this->importOrdersInPeriod((time()-86400) * 1000, (time()+86400) * 1000);
        $this->importNotFinishedOrders();
    }

    public function importNotFinishedOrders(): void
    {
        $data = $this->request->db->select_row_array("select distinct KaspiID from KaspiOrders where ShopID=? and StateID!=6 and StatusID!=4", [ $this->shopID ]);
        foreach ($data ?? [] as $row)
        {
            $this->importOrder($row['KaspiID']);
        }
    }

    public function importOrdersInPeriod(int $fromDate, int $toDate, $onlyNew = false): void
    {
        $statesCurrent = ['NEW','SIGN_REQUIRED','PICKUP','DELIVERY','KASPI_DELIVERY','ARCHIVE'];
        $statesOld = ['ARCHIVE'];
        if ($onlyNew)
        {
            $statesCurrent = ['NEW'];
        }

        $states = $fromDate < (time()-60*86400)*1000 ? $statesOld : $statesCurrent;
        $strDateFrom = date('Y-m-d', $fromDate / 1000);
        $strDateTo = date('Y-m-d', $toDate / 1000);

//        $this->request->db->do("update UserKaspiShops set CurrentImportOrdersDate=? where ID=?", [ $strDateFrom, $this->shopID ]);

        foreach ($states as $state)
        {
            $page = 0;
            while (true)
            {
//                print ("Importing orders for ShopID {$this->shopID} from {$strDateFrom} to {$strDateTo} with status {$state} page {$page}\n");
                $orders = $this->kaspiApi->getOrdersByStatusAndCreateDate($page, 100, $state, $fromDate, $toDate);

                if (!isset($orders['data']))
                {
//                    print('Error getting orders: ' . $this->kaspiApi->getErrorMessage());
                    break;
                }
                else
                {
                    if (!$this->kaspiApi->orderListResponseHashIsTheSame)
                    {
                        foreach ($this->kaspiApi->ordersListArray as $str)
                        {
                            $order = json_decode($str, true);
                            if (!isset($order['id']))
                            {
                                continue;
                            }

                            $orderHash = sha1($str);
                            $this->importOrder($order['id'], null, $orderHash, $order);
                        }
                        foreach ($orders['data'] as $order)
                        {
//                        print ("Importing order {$order['id']}\n");
//                            $this->importOrder($order['id']);
                        }
                    }

                    $page++;
                    if ($orders['meta']['pageCount'] === $page || !isset($orders['data']) || count($orders['data']) === 0)
                    {
                        break;
                    }
                }
            }
        }
    }

    public function processAndSyncCategories($data): array
    {
        $changed = 0;
        $errorsChanged = 0;
        $inserted = 0;
        $errorsInserted = 0;

        $localData = $this->request->db->select_row_array("select ID,Name,Code,CategoryID,Level,CommissionStart,CommissionEnd from KaspiCategoriesFromMenu");
        $localMapData = [];
        foreach ($localData as $category)
        {
            $localMapData[$category['Code']] = $category;
        }

        $localCategoryIDMapping = [];

        foreach ($data as $category)
        {
            $category['CommissionStart'] = $this->getCommissionWithNDS($category['CommissionStart']);
            $category['CommissionEnd'] = $this->getCommissionWithNDS($category['CommissionEnd']);
            $localCategory = $localMapData[$category['Code']] ?? null;
            if (isset($localCategory))
            {
                $localCategoryIDMapping[$category['ID']] = $localCategory['ID'];

                if ($localCategory['Name'] != $category['Name'] || $localCategory['CommissionStart'] != $category['CommissionStart'] || $localCategory['CommissionEnd'] != $category['CommissionEnd'])
                {
                    if ($this->request->db->do("update KaspiCategoriesFromMenu set Name=?, CommissionStart=?, CommissionEnd=? where ID=?",
                        [
                            $category['Name'],$category['CommissionStart'],$category['CommissionEnd'], $localCategory['ID']
                        ]))
                    {
                        $changed++;
                    }
                    else
                    {
                        $errorsChanged++;
                    }

                    if ($localCategory['CommissionStart'] != $category['CommissionStart'] || $localCategory['CommissionEnd'] != $category['CommissionEnd'])
                    {
                        $this->request->db->do("insert into KaspiCategoriesFromMenuCommissionHistory(CategoryID,CommissionStart,CommissionEnd) values (?,?,?)",
                            [
                                $localCategory['ID'],$category['CommissionStart'], $category['CommissionEnd']
                            ]);
                    }
                }
            }
            else
            {
                if ($this->request->db->do("insert into KaspiCategoriesFromMenu (Name,Code,Level,CategoryID,CommissionStart,CommissionEnd) values (?,?,?,?,?,?)",[ $category['Name'], $category['Code'], $category['Level'], $localCategoryIDMapping[$category['CategoryID']], $category['CommissionStart'], $category['CommissionEnd'] ]))
                {
                    $inserted++;
                    $insertid = $this->request->db->GetLastID();
                    $localCategoryIDMapping[$category['ID']] = $insertid;

                    $this->request->db->do("insert into KaspiCategoriesFromMenuCommissionHistory(CategoryID,CommissionStart,CommissionEnd) values (?,?,?)",
                        [
                            $insertid,$category['CommissionStart'], $category['CommissionEnd']
                        ]);
                }
            }
        }

        return compact('changed', 'inserted', 'errorsChanged', 'errorsInserted');
    }

    public function processAndSyncShopItems($data): array
    {
        $changed = 0;
        $errorsChanged = 0;
        $inserted = 0;
        $errorsInserted = 0;

        $cats = $this->request->db->select_row_array("select * from KaspiCategoriesFromMenu");
        $catsHash = [];
        foreach ($cats as $cat)
        {
            $catsHash[$cat['Code']] = $cat['ID'];
        }

        foreach ($data as $item)
        {
            $data = $this->request->db->select_row_array("select * from KaspiShopItems where KaspiID=?",[ $item['KaspiID'] ]);
            if (!isset($data[0]['ID']))
            {
                $res = $this->request->db->do("insert into KaspiShopItems (MenuCategoryID,KaspiID,Title,Brand,CategoryID,ShopLink,Price,CreatedTime,PreviewImage01,PreviewImage02,PreviewImage03,PreviewImage04,PreviewImage05,PreviewImage06,PreviewImage07,PreviewImage08,PreviewImage09,PreviewImage10,ReviewsLink,Rating,ReviewsQuantity,BaseProductCodes,ItemWeight) values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                    [
                        $catsHash[$item['CategoryCode']] ?? null,$item['KaspiID'], $item['Title'], $item['Brand'], $item['CategoryID'], $item['ShopLink'],
                        $item['Price'],$item['CreatedTime'],$item['PreviewImage01'],$item['PreviewImage02'],$item['PreviewImage03'],$item['PreviewImage04'],$item['PreviewImage05'],$item['PreviewImage06'],$item['PreviewImage07'],$item['PreviewImage08'],$item['PreviewImage09'],$item['PreviewImage10'],
                        $item['ReviewsLink'],$item['Rating'],$item['ReviewsQuantity'],$item['BaseProductCodes'],$item['ItemWeight']
                    ]);
                if ($res)
                {
                    $inserted++;
                }
                else
                {
                    $errorsInserted++;
                }
            }
            else
            {
                $res = $this->request->db->do("update KaspiShopItems set UpdatedDate=GetDate(),MenuCategoryID=?,KaspiID=?,Title=?,Brand=?,CategoryID=?,ShopLink=?,Price=?,CreatedTime=?,PreviewImage01=?,PreviewImage02=?,PreviewImage03=?,PreviewImage04=?,PreviewImage05=?,PreviewImage06=?,PreviewImage07=?,PreviewImage08=?,PreviewImage09=?,PreviewImage10=?,ReviewsLink=?,Rating=?,ReviewsQuantity=?,BaseProductCodes=?,ItemWeight=? where ID=?",
                    [
                        $catsHash[$item['CategoryCode']] ?? null,$item['KaspiID'], $item['Title'], $item['Brand'], $item['CategoryID'], $item['ShopLink'],
                        $item['Price'],$item['CreatedTime'],$item['PreviewImage01'],$item['PreviewImage02'],$item['PreviewImage03'],$item['PreviewImage04'],$item['PreviewImage05'],$item['PreviewImage06'],$item['PreviewImage07'],$item['PreviewImage08'],$item['PreviewImage09'],$item['PreviewImage10'],
                        $item['ReviewsLink'],$item['Rating'],$item['ReviewsQuantity'],$item['BaseProductCodes'],$item['ItemWeight'],$data[0]['ID']
                    ]);
                if ($res)
                {
                    $changed++;
                }
                else
                {
                    $errorsChanged++;
                }
            }

        }

        return compact('changed', 'inserted', 'errorsChanged', 'errorsInserted');
    }

    private function getStoreID($shopID, $originAddress, $pickupPointID): int|null
    {
        if (!isset($originAddress))
        {
            if (!isset($pickupPointID))
            {
                return null;
            }
            [$name, $code] = explode('_', $pickupPointID, 2);
            return $this->insertStore($shopID, $code, $code, '');
        }
        return $this->insertStore($shopID, $originAddress['displayName'], $originAddress['address']['formattedAddress'] ?? '', $originAddress['city']['name'] ?? '');
    }

    public function insertStore($shopID, $code, $name, $cityName): int|null
    {
        $data = $this->request->db->select_row_array("select * from UserKaspiShopsStores where ShopID=? and Code=? and DeletedDate IS NULL",[ $shopID, $code ]);
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into UserKaspiShopsStores (ShopID, Code, Name, CityName) values (?, ?, ?, ?)", [$shopID, $code, $name, $cityName]);

            return $this->request->db->GetLastID();
        }
        elseif ($data[0]['CityName'] != $cityName)
        {
            $this->request->db->do("update UserKaspiShopsStores set CityName=? where ID=?", [ $cityName, $data[0]['ID'] ]);
        }

        return $data[0]['ID'];
    }

    private function getSebesFromItemNal($productID, $storeID)
    {
        $sql = "select * from (
select sum(t.Coeff*o.Quant) as Quant,o.ProductID,o.StoreID,o.Sebes from UserKaspiShopsStoresOperations o 
inner join StoreOperationsTypes t on t.ID=o.OperationTypeID
where o.ProductID=? and StoreID=? and o.DeletedDate IS NULL and o.Sebes IS NOT NULL
and o.ID>=COALESCE((select top 1 OperationID from UserKaspiShopsStoresStartCount where StoreID=o.StoreID order by ID DESC),0)
group by o.ProductID,o.StoreID,o.Sebes
) as R
where R.Quant>0         
order by R.Sebes desc
";

        $data = $this->request->db->select_row_array($sql, [ $productID, $storeID ]);

        $totalQuant = 0;
        foreach ($data as $item)
        {
            $totalQuant += $item['Quant'];
        }

        return [$data[0]['Sebes'] ?? null, $totalQuant];
    }


    public function sendKaspiRequest($url, $sendHeaders, $data, $doPost = true): array
    {
        $start = microtime(true);
        $ch = curl_init();
        $headers = [];
        $setCookie = '';
        $location = '';

        if ($_ENV['USE_KASPI_LK_PROXY'] === 'true')
        {
            curl_setopt($ch, CURLOPT_PROXY, $_ENV['KASPI_PROXY_URL']);
            curl_setopt($ch, CURLOPT_PROXYUSERPWD, $_ENV['KASPI_PROXY_USERPWD']);
            curl_setopt($ch, CURLOPT_PROXYTYPE, CURLPROXY_HTTP);
            curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
            curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
        }

        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 60);
        curl_setopt($ch, CURLOPT_TIMEOUT, 60);
        curl_setopt($ch, CURLOPT_URL, $url);
        curl_setopt($ch, CURLOPT_HTTPHEADER, $sendHeaders);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, 1);
        if ($doPost) {
            curl_setopt($ch, CURLOPT_POST, true);
            curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode($data));
        }
        curl_setopt($ch, CURLOPT_ENCODING, "UTF-8");
        curl_setopt($ch, CURLOPT_HEADERFUNCTION, function ($curl, $header) use (&$headers, &$setCookie, &$location) {
            $len = strlen($header);
            $headers[] = $header;
            $header = explode(':', $header, 2);
            if (count($header) === 2 && $header[0] === 'set-cookie') {
                $setCookie = strtok(trim($header[1]), ';');
            }

            if (count($header) === 2 && $header[0] === 'location') {
                $location = strtok(trim($header[1]), ';');
            }

            return $len;
        });

        $response = curl_exec($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);

        curl_close($ch);

        $duration = (int)((microtime(true) - $start)*1000);

        $this->request->db->do("insert into KaspiAPILKCalls (URL,Headers,Payload,Response,ResponseHeaders,ResponseCode,Duration) values (?,?,?,?,?,?,?)",
            [
                $url,
                json_encode($sendHeaders),
                json_encode($data),
                $response,
                json_encode($headers),
                $httpCode,
                $duration
            ]);

        return [$response, $headers, $httpCode, $setCookie, $location];
    }

    public function getPersonalAccountCredentials($login, $password, $selectedMerchantID = null)
    {
        $tm = new TasksManagement($this->request);
        $data = [ 'Email' => $login, 'Password'=>$password ];
        if (isset($selectedMerchantID))
        {
            $data['MerchantID'] = $selectedMerchantID;
        }
        $id = $tm->insertTaskForUser('lkAccessCheck', $this->request->userID, json_encode($data));
        $start = microtime(true);
        do
        {
            sleep(1);
            $taskData = $tm->getTaskJustById($id);
            if (isset($taskData['FinishedDate']))
            {
                return json_decode($taskData['DistrResult'], true);
            }
        }
        while ((microtime(true) - $start) < 15);

        $ua = "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0";
        $acceptLanguage = "Accept-Language: en-US,en;q=0.5";
        $acceptAll = "Accept: application/json, text/plain, */*";
        $acceptXml = "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8";
        $acceptEnc = "Accept-Encoding: gzip, deflate, br, zstd";
        $contentTypeJSON = "Content-Type: application/json";
        $keepAlive = "Connection: keep-alive";
        $refIdmcLogin = "Referer: https://idmc.shop.kaspi.kz/login";
        $originIdmc = "Origin: https://idmc.shop.kaspi.kz";
        $refKaspi = "Referer: https://kaspi.kz/";
        $refIdmc = "Referer: https://idmc.shop.kaspi.kz/";
        $refKaspiMc = "Referer: https://kaspi.kz/mc/";

        $sec1 = ["DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-origin", "Priority: u=0", "TE: trailers"];
        $sec2 = ["DNT: 1", "Sec-GPC: 1", "Upgrade-Insecure-Requests: 1", "Sec-Fetch-Dest: document", "Sec-Fetch-Mode: navigate", "Sec-Fetch-Site: same-site", "Priority: u=0, i"];
        $sec3 = ["Upgrade-Insecure-Requests: 1", "Sec-Fetch-Dest: document", "Sec-Fetch-Mode: navigate", "Sec-Fetch-Site: same-origin", "Sec-Fetch-User: ?1", "Priority: u=0, i", "TE: trailers"];
        $sec4 = ["x-auth-version: 3", "DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-site", "Priority: u=4", "TE: trailers"];
        $sec5 = ["DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-origin"];
        $sec6 = ["DNT: 1", "Sec-GPC: 1", "X-Auth-Version: 3", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-site", "TE: trailers"];
        $originKaspi = "Origin: https://kaspi.kz";

        $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc];
        [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/login", $headers, [], false);
        $step = 1;

        if ($httpCode == 200)
        {
            $step = 2;
            $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $contentTypeJSON, $originIdmc, $keepAlive, $refIdmcLogin, ...$sec1];
            [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/api/p/login", $headers, ['_u' => $login, '_p' => $password]);

            if ($httpCode == 200)
            {
                $step = 3;
                $r = json_decode($response, true);
                if (isset($r))
                {
                    $step = 4;
                    $MS_AUTH_SSO_Cookie = $setCookie;
                    $headers = ["Cookie: " . $setCookie, $ua, $acceptXml, $acceptLanguage, $acceptEnc, $keepAlive, $refIdmcLogin, ...$sec3];
                    [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz" . $r['redirectUrl'], $headers, [], false);

                    if ($httpCode == 302)
                    {
                        $step = 5;
                        $headers = [$ua, $acceptXml, $acceptLanguage, $acceptEnc, $refIdmc, $keepAlive, ...$sec2, "Sec-Fetch-User: ?1"];
                        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($location, $headers, [], false);

                        if ($httpCode == 200)
                        {
                            $step = 6;
                            $secondPart = GenerateSessionID(9);
                            $ampCookie = 'amp_6e9c16=' . GenerateSessionID(10) . '-' . GenerateSessionID(11) . '...' . $secondPart . '.' . $secondPart;
                            $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $keepAlive, $refKaspiMc, "Cookie: {$ampCookie}.0.0.0", ...$sec5];
                            [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://kaspi.kz/yml/ms/feat/p/ft/pre/e?d=true", $headers, [], false);

                            if ($httpCode == 200)
                            {
                                $step = 7;
                                $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $originKaspi, $keepAlive, $refKaspi,"Cookie: {$ampCookie}.0.0.0", ...$sec6];
                                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/s/m", $headers, [], false);

                                if ($httpCode == 401)
                                {
                                    $step = 8;
                                    $mc_session_cookie = $setCookie;
                                    $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $originKaspi, $keepAlive, $refKaspi,"Cookie: {$ampCookie}.1.0.1; " . $setCookie, ...$sec6];
                                    [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/s/m", $headers, [], false);

                                    if ($httpCode == 401)
                                    {
                                        $step = 9;
                                        $mc_session_cookie = $setCookie;
                                        $headers = [$ua, $acceptXml, $acceptLanguage, $acceptEnc, $keepAlive, $refKaspi,"Cookie: {$ampCookie}.1.0.1; " . $setCookie, ...$sec2, "TE: trailers"];
                                        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/oauth2/authorization/1", $headers, [], false);

                                        if ($httpCode == 302)
                                        {
                                            $step = 10;
                                            $mc_arr_cookie = $setCookie;
                                            $headers = [$ua, $acceptXml, $acceptLanguage, $acceptEnc, $keepAlive,"Cookie:{$MS_AUTH_SSO_Cookie}; {$ampCookie}.1.0.1", ...$sec2, "TE: trailers"];
                                            [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($location, $headers, [], false);

                                            if ($httpCode == 302)
                                            {
                                                $step = 11;
                                                $headers = [$ua, $acceptXml, $acceptLanguage, $acceptEnc, $keepAlive,"Cookie: {$ampCookie}.1.0.1; {$mc_arr_cookie}; {$mc_session_cookie}", ...$sec2, "TE: trailers"];
                                                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($location, $headers, [], false);

                                                if ($httpCode == 302)
                                                {
                                                    $step = 12;
                                                    $mc_sid_cookie = $setCookie;
                                                    $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.2.0.2; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                                                    [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/s/m", $headers, [], false);

                                                    if ($httpCode == 200)
                                                    {
                                                        $step = 13;
                                                        $r = json_decode($response, true);
                                                        $merchantId = $r['merchants'][0]['uid'];
                                                        $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.3.0.3; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                                                        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/bff/offer-view/list?m={$merchantId}&p=0&l=10&a=true&t=&c=&lowStock=false", $headers, [], false);
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        return ['step' => $step, 'merchantId' => $merchantId ?? null, 'ampCookie' =>$ampCookie ?? null, 'mc_session_cookie' => $mc_session_cookie ?? null, 'mc_sid_cookie' => $mc_sid_cookie ?? null];
    }

    public function validatePersonalCabinetCreds($creds, $justReceived = false): bool
    {
        if ($justReceived)
        {
            if ((!isset($creds['step']) || $creds['step'] != 13))
                return false;
        }

        return isset( $creds['merchantId'], $creds['ampCookie'], $creds['mc_session_cookie'], $creds['mc_sid_cookie'] );
    }

    public function getLKCredentialsFromArray($data)
    {
        $this->merchantId = $data['merchantId'] ?? null;
        $this->ampCookie = $data['ampCookie'] ?? null;
        $this->mc_session_cookie = $data['mc_session_cookie'] ?? null;
        $this->mc_sid_cookie = $data['mc_sid_cookie'] ?? null;
    }

    public function getPersonalCabinetRequestHeaders($requestCode)
    {
        $ua = "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0";
        $acceptLanguage = "Accept-Language: en-US,en;q=0.5";
        $acceptEnc = "Accept-Encoding: gzip, deflate, br, zstd";
        $contentTypeJSON = "Content-Type: application/json";
        $keepAlive = "Connection: keep-alive";
        $refKaspi = "Referer: https://kaspi.kz/";
        $sec4 = ["x-auth-version: 3", "DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-site", "Priority: u=4", "TE: trailers"];
        $originKaspi = "Origin: https://kaspi.kz";

        return [$ua, "Accept: application/json, text/plain, */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$this->ampCookie}.{$requestCode}; {$this->mc_session_cookie}; {$this->mc_sid_cookie}", ...$sec4];
    }

    public function getPersonalCabinetItems($page = 0, $perPage = 10, $onlyActive = null)
    {
        $active = "";
        if (isset($onlyActive))
        {
            $active = $onlyActive ? "&a=true" : "&a=false";
        }
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/bff/offer-view/list?m={$this->merchantId}&p={$page}&l={$perPage}{$active}", $this->getPersonalCabinetRequestHeaders("1s.0.1s"), [], false);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r;
    }

    public function getAllPersonalCabinetItems($perPage = 100, $onlyActive = null)
    {
        $allData = [];
        $page = 0;
        $data = $this->getPersonalCabinetItems($page, $perPage, $onlyActive);
        if (!isset($data['data']))
        {
            return null;
        }

        while (isset($data['data']) && count($data['data']) > 0)
        {
            $allData = array_merge($allData, $data['data']);
            $page++;
            $data = $this->getPersonalCabinetItems($page, $perPage, $onlyActive);
        }
        return $allData;
    }

    public function getPersonalCabinetStores()
    {
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=getPointList",$this->getPersonalCabinetRequestHeaders("1c.0.1c"), ["operationName"=>"getPointList","variables"=>["merchantUid"=>$this->merchantId],"query"=>"query getPointList(\$merchantUid: String!) {\n  merchant(id: \$merchantUid) {\n    id\n    points {\n      id\n      enabled\n      city {\n        name\n        __typename\n      }\n      address {\n        streetName\n        streetNumber\n        building\n        phone\n        name\n        __typename\n      }\n      type\n      schedule {\n        weekDays {\n          openingTime\n          closingTime\n          dayOfWeek\n          __typename\n        }\n        __typename\n      }\n      __typename\n    }\n    __typename\n  }\n}\n"]);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r;
    }

    public function updateShopStoresFromPersonalCabinetData($data): void
    {
        foreach ($data['data']['merchant']['points'] ?? [] as $store)
        {
            $id = $store['id'];
            [$merchantID, $code] = explode('_', $id);
            $this->insertStore($this->shopID, $code, $store['city']['name'] ?? '', $store['city']['name'] ?? '');
        }
    }

    public function insertUpdateMasterProduct($code, $name, $images = null): array
    {
        $data = $this->request->db->select_row_array("select *,(select TOP 1 ID from KaspiMasterProductsVesions where MasterProductID=a.ID order by ID DESC) as VersionID from KaspiMasterProducts a where Code=?",[ $code ]);
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into KaspiMasterProducts (KaspiID, Name, Code) values (?, ?, ?)",[ '', $name, $code ]);
            $masterProductID = $this->request->db->GetLastID();

            $this->request->db->do("insert into KaspiMasterProductsVesions (MasterProductID, KaspiID, Name, Code) values (?, ?, ?, ?)",[ $masterProductID, '', $name, $code ]);
            $versionID = $this->request->db->GetLastID();
        }
        else
        {
            $masterProductID = (int)$data[0]['ID'];
            $versionID = (int)$data[0]['VersionID'];
            if ($data[0]['Name'] != $name)
            {
                $this->request->db->do("update KaspiMasterProducts set Name=? where ID=?",[ $name, $masterProductID ]);
                $this->request->db->do("insert into KaspiMasterProductsVesions (MasterProductID, KaspiID, Name, Code) values (?, ?, ?, ?)",[ $masterProductID, '', $name, $code ]);
                $versionID = $this->request->db->GetLastID();
            }

            if (!isset($versionID))
            {
                $this->request->db->do("insert into KaspiMasterProductsVesions (MasterProductID, KaspiID, Name, Code) values (?, ?, ?, ?)",[ $masterProductID, '', $name, $code ]);
                $versionID = $this->request->db->GetLastID();
            }
        }

        if (isset($masterProductID) && !empty($images))
        {
            $this->request->db->do("update KaspiMasterProducts set FirstImage=?, Images=? where ID=?",[ $images[0], implode(',', $images) , $masterProductID ]);
        }

        return [$masterProductID, $versionID];
    }

    public function insertUpdateMerchantProduct($masterProductID, $code, $name, $storeID = null, $brand = null, $fromKaspi = false, $isAvailable = null, $price = null, $minPrice = null): array
    {
        $sebesFromNal = false;
        if (isset($price))
        {
            if ($price == 0 && isset($minPrice) && $minPrice > 0) {
                $price = $minPrice;
            }
        }
        else
        {
            if (isset($minPrice) && $minPrice > 0)
            {
                $price = $minPrice;
            }
        }

        $itemTotalQuant = 0;

        $data = $this->request->db->select_row_array("select *,(select TOP 1 ID from KaspiMerchantProductsVersions where MerchantProductID=a.ID order by ID DESC) as VersionID from KaspiMerchantProducts a where Code =? and UserID=? and ShopID=?",[ $code, $this->userID, $this->shopID ]);
        $currentSebes = null;
        $parentProductID = null;
        $wasCreatedNew = false;
        if (!isset($data[0]['ID']))
        {
            $this->request->db->do("insert into KaspiMerchantProducts (KaspiMasterProductID, Code, Name, UserID, ShopID, Brand) values (?, ?, ?, ?, ?, ?)",[ $masterProductID, $code, $name, $this->userID, $this->shopID, $brand ]);
            $productID = $this->request->db->GetLastID();

            $wasCreatedNew=true;

            $this->request->db->do("insert into KaspiMerchantProductsVersions (MerchantProductID, Code, Name) values (?, ?, ?)",[ $productID, $code, $name ]);
            $productVersionID = $this->request->db->GetLastID();

            if ($price > 0)
                $this->request->db->do("update KaspiMerchantProducts set Price=? where ID=? and ShopID=?", [ $price, $productID, $this->shopID ]);
        }
        else
        {
            $productID = (int)$data[0]['ID'];
            $productVersionID = (int)$data[0]['VersionID'];
            $parentProductID = isset($data[0]['ParentMerchantID']) ? (int)$data[0]['ParentMerchantID'] : null;

//            $currentSebes = $data[0]['CurrentSebes'];
            if (isset($storeID))
            {
                [ $sebes, $totalQuant ] = $this->getSebesFromItemNal($parentProductID ?? $productID, $storeID);
                if (isset($totalQuant) && $totalQuant > 0)
                    $itemTotalQuant = $totalQuant;

                if (isset($sebes))
                {
                    $currentSebes = $sebes;
                    $sebesFromNal = true;
                }
            }

            if ($data[0]['KaspiMasterProductID'] != $masterProductID)
            {
                $this->request->db->do("update KaspiMerchantProducts set KaspiMasterProductID=? where ID=?",[ $masterProductID, $productID ]);
            }

            if ($data[0]['Name'] != $name && isset($name, $code, $productID))
            {
                $this->request->db->do("insert into KaspiMerchantProductsVersions (MerchantProductID, Code, Name) values (?, ?, ?)",[ $productID, $code, $name ]);
                $productVersionID = $this->request->db->GetLastID();
                $this->request->db->do("update KaspiMerchantProducts set Name=? where ID=?",[ $name, $productID ]);
            }

            if (isset($brand) && $data[0]['Brand'] != $brand)
            {
                $this->request->db->do("update KaspiMerchantProducts set Brand=? where ID=?",[ $brand, $productID ]);
            }

            if ($price > 0 && !isset($data[0]['Price']))
                $this->request->db->do("update KaspiMerchantProducts set Price=? where ID=? and ShopID=?", [ $price, $productID, $this->shopID ]);
        }

        if ($fromKaspi && isset($productID))
        {
            $params = [ $productID ];
            $addSql = '';
            if (isset($isAvailable))
            {
                $params = [ $isAvailable ? 1 : 0, $productID ];
                $addSql = ', IsAvailable=? ';
            }
            $this->request->db->do("update KaspiMerchantProducts set LastUpdateCheckDate=GetDate() {$addSql} where ID=?", $params);
        }

        return [$productID, $productVersionID, $currentSebes, $sebesFromNal, $parentProductID, $itemTotalQuant, $wasCreatedNew];
    }

    public function updateProductCommission($productID, $masterCategory): void
    {
        $data = $this->request->db->select_row_array("select top 1 CommissionWithNDS from KaspiCategoriesDocPercent where Cat5=(select top 1 Name from KaspiOrderItemCategories where Code=?)
or Cat5=(select top 1 Name from KaspiCategoriesFromMenu where Code=?)",[ $masterCategory, str_replace('Master - ', '', $masterCategory) ]);

        if (isset($data[0]['CommissionWithNDS']))
        {
            $this->request->db->do("update KaspiMerchantProducts set CategoryCommission=? where ID=?",[ $data[0]['CommissionWithNDS'], $productID ]);
        }
    }

    public function updateShopItemsFromPersonalCabinetData($data, $afterRegistration = false): void
    {
        $storesData = null;
        if ($afterRegistration)
        {
            $loadStoresData = $this->request->db->select_row_array("select * from UserKaspiShopsStores where ShopID=? and DeletedDate IS NULL",[ $this->shopID ]);
            $storesData = [];
            foreach ($loadStoresData as $store)
            {
                $storesData[$store['Code']] = $store['ID'];
            }
        }

        foreach ($data ?? [] as $item)
        {
            [$masterProductID, $versionID] = $this->insertUpdateMasterProduct($item['masterSku'], $item['masterTitle'], $item['images'] ?? null);
            [$productID, $productVersionID, $currentSebes, $sebesFromNal, $parentProductID, $itemTotalQuant, $wasCreatedNew] = $this->insertUpdateMerchantProduct($masterProductID, $item['sku'], isset($item['model']) ? $item['model'] : ( $item['title'] ?? $item['masterTitle']),
                null, $item['brand'] ?? null, true, $item['available'] ?? null,$item['price'] ?? null, $item['minPrice'] ?? null);

            if ($wasCreatedNew && !isset($storesData))
            {
                $loadStoresData = $this->request->db->select_row_array("select * from UserKaspiShopsStores where ShopID=? and DeletedDate IS NULL",[ $this->shopID ]);
                $storesData = [];
                foreach ($loadStoresData as $store)
                {
                    $storesData[$store['Code']] = $store['ID'];
                }
            }

            if (isset($productID) && !empty($item['masterCategory']))
            {
                $this->updateProductCommission($productID, $item['masterCategory']);
            }

            if (($afterRegistration || $wasCreatedNew) && isset($productID) && isset($item['available']) && $item['available'])
            {
                try {
                    $this->processAvailabilities($productID, $item['availabilities'] ?? [], $item['stocks'] ?? [], $storesData, $item['price'] ?? null, $item['minPrice'] ?? null);
                }
                catch (Throwable $e)
                {
                    print "Error processAvailabilities for $productID: " . $e->getMessage() . "\n";
                }
            }
        }
    }

    public function processAvailabilities($productID, $availabilities, $stocks, $storesData, $price, $minPrice): void
    {
        if (isset($price))
        {
            if ($price == 0 && isset($minPrice) && $minPrice > 0)
            {
                $price = $minPrice;
            }
            if ($price > 0)
                $this->request->db->do("update KaspiMerchantProducts set Price=? where ID=? and ShopID=?", [ $price, $productID, $this->shopID ]);
        }

        if (count($availabilities) === 0)
            return;

        $availabilitiesData =[];
        foreach ($availabilities as $availability)
        {
            [$name, $code] = explode('_', $availability['storeId'], 2);
            $availabilitiesData[$code] = $availability;
        }

        $stocksData =[];
        foreach ($stocks as $stock)
        {
            foreach ($stock['stockLevel'] as $key => $value)
            {
                [$name, $code] = explode('_', $key, 2);
                $stocksData[$code] = $value;
            }
        }

//        print "availabilitiesData for $productID\n";
//        var_dump($availabilitiesData);

//        print "stocksData for $productID\n";
//        var_dump($stocksData);

//        print "stocks for $productID\n";
//        var_dump($stocks);

        foreach ($availabilitiesData as $code => $availability)
        {
            if ($availability['available'] === 'yes')
            {
                $storeID = $storesData[$code] ?? null;
                if (isset($storeID))
                {
                    $preOrder = $availability['preOrder'] ?? 0;
                    if ($availability['stockSpecified'])
                    {
                        $stockCount = $stocksData[$code]['value'] ?? null;
                    }
                    else
                    {
                        $stockCount = 5;
                    }

                    if ( isset($stockCount) && $stockCount > 0 )
                    {
//                        $this->request->db->do("insert into UserKaspiShopsStoresOperations (ProductID,StoreID,Quant,Sebes,Price,OperationTypeID) values (?,?,?,?,?,?)", [$productID, $storeID, $stockCount,(int)($price*0.7), $price, 1]);
                        $this->request->db->do("insert into UserKaspiShopsStoresOperations (ProductID,StoreID,Quant,Price,OperationTypeID) values (?,?,?,?,?)", [$productID, $storeID, $stockCount, $price, 1]);

                    }

                    if ($preOrder > 0)
                    {
                        $this->request->db->do("insert into KaspiMerchantProductStorePreOrder (ProductID,StoreID,PreOrderDays) values (?,?,?)", [$productID, $storeID, $preOrder]);
                    }
                }
            }
        }
    }

    public function setItemPrice($sku, $price, $storecodes)
    {
        //curl 'https://mc.shop.kaspi.kz/pricefeed/upload/merchant/process' --compressed -X POST -H 'User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:138.0) Gecko/20100101 Firefox/138.0' -H 'Accept: application/json, text/plain, */*' -H 'Accept-Language: en-US,en;q=0.5' -H 'Accept-Encoding: gzip, deflate, br, zstd' -H 'Referer: https://kaspi.kz/' -H 'Origin: https://kaspi.kz' -H 'Connection: keep-alive' -H 'Cookie: _ga_0R30CM934D=GS1.1.1733303617.8.0.1733303617.60.0.0; _ga=GA1.1.1622609083.1720434670; ssaid=3191a1d0-3d15-11ef-afd4-83b58425f4d2; test.user.group=88; test.user.group_exp=20; test.user.group_exp2=20; _ga_6273EB2NKQ=GS1.1.1724581697.1.1.1724582973.0.0.0; _hjSessionUser_283363=eyJpZCI6ImRiYjc4YjYxLTNmNjQtNTAxMS1iZWJiLTg2MTUyMzIzMjQyOCIsImNyZWF0ZWQiOjE3MzI2ODMyOTI0ODgsImV4aXN0aW5nIjp0cnVlfQ==; _hjid=abf850a6-01cb-4997-9507-69a7285130e3; _ym_uid=1612412173122590540; _ym_d=1733299776; _ga_1RJKXYPLPK=GS1.2.1733299776.1.0.1733299776.60.0.0; amp_6e9c16=fMiMFwSCoI-VhLlSpQ13Ut...1iqikfkea.1iqikl3rg.f.0.f; mc-session=1746529671.617.2772.482503|825e5f3659dba1ed7b5d7b2cbf5f1012; mc-sid=70a66593-1ca3-4e2a-af85-e56abd5cd19b' -H 'Sec-Fetch-Dest: empty' -H 'Sec-Fetch-Mode: no-cors' -H 'Sec-Fetch-Site: same-site' -H 'TE: trailers' -H 'Content-Type: application/json' -H 'X-Auth-Version: 3' -H 'Priority: u=4' -H 'Pragma: no-cache' -H 'Cache-Control: no-cache' --data-raw '{"merchantUid":"30297603","sku":"061077339","price":330}'
        //"availabilities":[{"available":"yes","storeId":"30297603_PP1","stockEnabled":false}],
        $availabilities = [];
        foreach ($storecodes as $storecode)
        {
            $availabilities[] = ["available" => "yes", "storeId" => $this->merchantId . '_' . $storecode, "stockEnabled" => false];
        }

        $data = ["merchantUid" => $this->merchantId,"sku" => $sku, "price" => $price, 'availabilities' => $availabilities];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/pricefeed/upload/merchant/process",
            $this->getPersonalCabinetRequestHeaders("f.0.f"),
            $data, true);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r;
    }

    public function checkAccessAndGetNewIfNotValid(): bool
    {
        if ($this->getPersonalCabinetStores() != null)
        {
            return true;
        }

        if (isset($this->lkLogin, $this->lkPassword))
        {
            $creds = $this->getPersonalAccountCredentials($this->lkLogin, $this->lkPassword);
            if ($this->validatePersonalCabinetCreds($creds, true))
            {
                $this->request->db->do("update UserKaspiShops set UseLKAccess=?, LKAccessDataRenewDate=GETDATE(), LKAccessActive=?, LKAccessData=? where ID = ?", [ 1, 1, json_encode($creds), $this->shopID ]);
                $this->getLKCredentialsFromArray($creds);
                return true;
            }
        }

        return false;
    }

    public static function getKaspiItemMinPrice($itemID, $count = 5, $justReturnData = false): array|null
    {
        $ch = curl_init();

        $headers = [
            "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0",
            "Accept: application/json, text/plain, */*",
            "Accept-Language: en-US,en;q=0.5",
            "Accept-Encoding: gzip, deflate, br, zstd",
            "Referer: https://kaspi.kz/shop/p/-{$itemID}/",
            "Content-Type: application/json; charset=utf-8"
        ];

        if ($_ENV['USE_KASPI_PARSING_PROXY'] === 'true')
        {
            curl_setopt($ch, CURLOPT_PROXY, $_ENV['KASPI_PARSING_PROXY']);
            curl_setopt($ch, CURLOPT_PROXYUSERPWD, $_ENV['KASPI_PARSING_PROXY_USERPWD']);
            curl_setopt($ch, CURLOPT_PROXYTYPE, CURLPROXY_HTTP);
            curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
            curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
        }

        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 10);
        curl_setopt($ch, CURLOPT_TIMEOUT, 50);
        curl_setopt($ch, CURLOPT_URL, "https://kaspi.kz/yml/offer-view/offers/{$itemID}");
        curl_setopt($ch, CURLOPT_HTTPHEADER,$headers);
        curl_setopt($ch, CURLOPT_ENCODING ,"UTF-8");
        curl_setopt($ch, CURLOPT_POST, true);
        curl_setopt($ch, CURLOPT_POSTFIELDS, '{"cityId":"750000000","id":"' . $itemID . '","limit":' . $count . ',"page":0,"sortOption":"PRICE"}');
        curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, 0);
        curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, 0);

        $response = curl_exec($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $price = null;
        $merchantId = null;
        if ($httpCode == 200)
        {
            $data = json_decode($response, true);
            if ($justReturnData)
                return $data;

            if (isset($data['offers']) && count($data['offers']) > 0)
            {
                $price = (float)$data['offers'][0]['price'];
                $merchantId = $data['offers'][0]['merchantId'];
            }
        }
        else
        {
            print "Code returned {$httpCode} for item {$itemID}, response {$response}\n";
            return null;
        }

        return [$price, $merchantId];
    }

    public function getOrderStatusID($state, $status)
    {
        $orderStatusID = null;
        if ($state === 'NEW' || ($state == 'SIGN_REQUIRED' && $status =='ACCEPTED_BY_MERCHANT'))
            $orderStatusID = 1;
        if ($state === 'PICKUP')
            $orderStatusID = 2;
        if (in_array($state, [ 'KASPI_DELIVERY','DELIVERY' ]))
            $orderStatusID = 3;
        if ($state === 'ARCHIVE' && $status === 'COMPLETED')
            $orderStatusID = 6;
        if (in_array($status, ['CANCELLED', 'CANCELLING']))
            $orderStatusID = 4;
        if (in_array($status, ['KASPI_DELIVERY_RETURN_REQUESTED', 'RETURNED']))
            $orderStatusID = 5;

        if (!isset($orderStatusID))
        {
            print "Unknown state/status combination: {$state}/{$status}\n";
        }

        return $orderStatusID;
    }

    public function updateOrdersStatuses()
    {
        $sql = "select top 100 o.ID,s.Name as State,st.Name as Status from KaspiOrders o
inner join KaspiOrderStates s on s.ID=o.StateID 
inner join KaspiOrderStatuses st on st.ID=o.StatusID
where OrderStatusID IS NULL";

        do
        {
            $data = $this->request->db->select_row_array($sql);
            if (count($data) == 0)
                break;

            foreach ($data as $item)
            {
                $orderStatusID = $this->getOrderStatusID($item['State'], $item['Status']);
                if (isset($orderStatusID))
                {
                    $this->request->db->do("update KaspiOrders set OrderStatusID=? where ID=?", [$orderStatusID, $item['ID']]);
                }
            }
        } while (true);
    }

    private function wholeOrderStatusChanged($orderID, $OrderStatusID, $NewOrderStatusID): void
    {
        if (isset($NewOrderStatusID) && $OrderStatusID != $NewOrderStatusID)
        {
            $this->request->db->do("insert into OrderStatusesHistory (OrderID, StatusID) values (?, ?)", [$orderID, $NewOrderStatusID]);
/*
            if ($NewOrderStatusID == 4 || $NewOrderStatusID == 5)
            {
                $data = $this->request->db->select_row_array("select o.* from UserKaspiShopsStoresOperations o inner join KaspiOrderDetails d on d.ID=o.KaspiOrderDetailID where d.KaspiOrderID=? and OperationTypeID=2", [$orderID]);
                foreach ($data ?? [] as $item)
                {
                    $dataReturned = $this->request->db->select_row_array("select top 1 * from UserKaspiShopsStoresOperations where KaspiOrderDetailID=? and OperationTypeID=4 and ReasonID=4 order by ID DESC", [ $item['KaspiOrderDetailID'] ]);
                    if (!isset($dataReturned[0]['ID']))
                    {
                        $this->request->db->do("insert into UserKaspiShopsStoresOperations (ProductID,StoreID,Quant,Price,Sebes,OperationTypeID,KaspiOrderDetailID,ReasonID,SourceID) values (?, ?, ?, ?, ?, 4, ?, 4, 1)",
                            [ $item['ProductID'] , $item['StoreID'], $item['Quant'], $item['Price'], $item['Sebes'], $item['KaspiOrderDetailID'] ]);
                    }
                }
            }
*/
        }
    }

    public function marketingLogin($login, $password, $merchantid)
    {
        $tm = new TasksManagement($this->request);
        $id = $tm->insertTaskForUser('marketingAccessCheck', $this->request->userID, json_encode(['Login' => $login, 'Password'=>$password, 'MerchantID'=>$merchantid ]));
        $start = microtime(true);
        do
        {
            sleep(1);
            $taskData = $tm->getTaskJustById($id);
            if (isset($taskData['FinishedDate']))
            {
                return json_decode($taskData['DistrResult'], true);
            }
        }
        while ((microtime(true) - $start) < 15);

        return [ 'success' => false, 'cookie' => null, 'merchantid' => null ];
    }

    public function validateMarketingCreds($creds): bool
    {
        return isset( $creds['success'], $creds['cookie'], $creds['merchantid'] ) && $creds['success'] === true;
    }

    public function getPersonalCabinetOrders($fromDate, $toDate, $page = 0, $perPage = 10)
    {
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/api/orderTabs/archive?start=" . ($page * $perPage) . "&count={$perPage}&fromDate={$fromDate}&toDate={$toDate}&statuses=CANCELLED&statuses=COMPLETED&statuses=RETURNED&statuses=CREDIT_TERMINATION_PROCESS&_m={$this->merchantId}", $this->getPersonalCabinetRequestHeaders("50.0.50"), [], false);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r;
    }

    private function getPersonalCabinetAllOrders($startDate, $endDate)
    {
        $allData = [];

        $fromDate = $startDate * 1000;
        $toDate = $fromDate + 89 * 86400 * 1000;

        do
        {
            $page = 0;
            do
            {
                $data = $this->getPersonalCabinetOrders($fromDate, $toDate, $page);
                $allData = array_merge($allData, $data['orders'] ?? []);
                $page++;
            }
            while (isset($data['orders']) && count($data['orders']) > 0);

            $fromDate = $toDate;
            $toDate += 89 * 86400 * 1000;

        } while ($toDate < $endDate * 1000 + 89 * 86400 * 1000);

        return $allData;
    }

    public function getPersonalCabinetOrderIssuedDate($orderCode)
    {
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/api/order/{$orderCode}?_m={$this->merchantId}", $this->getPersonalCabinetRequestHeaders("2j.0.2j"), [], false);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
            return $r['issuedDate'] ?? $r['cancellationDate'] ?? null;

        }
        return $r;
    }

    public function getPersonalCabinetAllOrdersFinishDate($startDate, $endDate)
    {
        $allData = [];

        $fromDate = $startDate * 1000;
        $toDate = $fromDate + 89 * 86400 * 1000;

        do
        {
            $page = 0;
            do
            {
                $data = $this->getPersonalCabinetOrders($fromDate, $toDate, $page);
                foreach ($data['orders'] ?? [] as $order)
                {
                    if (!isset($order['delivery']['actualDeliveryDate']))
                    {
                        $issuedDate = $this->getPersonalCabinetOrderIssuedDate($order['orderCode']);
                        $order['delivery']['actualDeliveryDate'] = $issuedDate;
                    }
                    if (isset($order['delivery']['actualDeliveryDate']))
                        $allData[] = [ $order['orderCode'], $order['delivery']['actualDeliveryDate'] ];
                }
                $page++;
            }
            while (isset($data['orders']) && count($data['orders']) > 0);

            $fromDate = $toDate;
            $toDate += 89 * 86400 * 1000;

        } while ($toDate < $endDate * 1000 + 89 * 86400 * 1000);

        return $allData;
    }

    public static function setMasterProductPrices($request, $code, $cityOffers, $noDumping = false): int
    {
        $updates = 0;

        $productData = $request->db->select_row_array("select ID from KaspiMasterProducts where Code=?", [$code]);
        if (!isset($productData[0]['ID'])) {
            return 0;
        }

        $masterProductID = $productData[0]['ID'];
        if ($noDumping)
        {
            $request->db->do("update KaspiMasterProducts set LastPriceNoDumpingCheckDate=GetDate(), OffersCount=? where ID=?", [$data['total'] ?? null, $masterProductID]);
        }
        else
        {
            $request->db->do("update KaspiMasterProducts set LastPriceCheckDate=GetDate(), OffersCount=?, PreviousPriceUpdateDate=LastPriceUpdateDate, LastPriceUpdateDate=GetDate() where ID=?", [$data['total'] ?? null, $masterProductID]);
        }

        foreach ($cityOffers as $cityOffer)
        {
            $cityCode = $cityOffer['cityCode'] ?? null;
            if (empty($cityCode))
                continue;

            $cityData = $request->db->select_row_array("select ID from KaspiCities where Code=?", [$cityCode]);
            if (!isset($cityData[0]['ID']))
                continue;

            $cityID = $cityData[0]['ID'];

            try {
                $placesData = $request->db->select_row_array("select ID,Price,MerchantID,MerchantName,Place,Rating,Reviews,Preorder,Delivery,DeliveryDays from KaspiMasterProductPriceHistory where MasterProductID=? and CityID=? order by Place", [ $masterProductID, $cityID ]);
            }
            catch (Throwable $e){
                continue;
            }

            $oldPlacesData = [];
            $place = 0;
            $maxPlace = 0;
            foreach ($placesData ?? [] as $placeData)
            {
                $maxPlace = $placeData['Place'];
                $oldPlacesData[$placeData['Place']] = [ $placeData ];
            }

            foreach ($cityOffer['offers'] ?? [] as $item)
            {
                $place++;
                $price = (int)$item['price'];
                $merchantId = $item['merchantId'];
                $merchantName = $item['merchantName'];
                $offers = $item['total'] ??  null;

                $rating = $item['rating'] ??  null;
                $reviews = $item['reviews'] ??  null;
                $delivery = $item['deliveryDuration'] ??  null;
                $preorder = $item['preorder'] ??  null;
                $days = $item['days'] ??  null;

                $oldData = $oldPlacesData[$place] ?? null;
                if (isset($oldData[0]['ID']))
                {
                    $oldPrice = (int)$oldData[0]['Price'];
                    if ($oldPrice === $price && $oldData[0]['MerchantID'] === $merchantId && $oldData[0]['MerchantName'] == $merchantName && $oldData[0]['Rating'] == $rating && $oldData[0]['Reviews'] == $reviews && $oldData[0]['Delivery'] == $delivery && $oldData[0]['Preorder'] == $preorder && $oldData[0]['DeliveryDays'] == $days)
                        continue;

                    $request->db->do("update KaspiMasterProductPriceHistory set Date=GetDate(), Price=?, MerchantID=?, MerchantName=?, OffersCount=?, Rating=?, Reviews=?, Preorder=?, Delivery=?, DeliveryDays=? where ID=?",
                        [$price, $merchantId, $merchantName, $offers, $rating, $reviews, $preorder, $delivery, $days, $oldData[0]['ID']]);
                }
                else
                {
                    $request->db->do("insert into KaspiMasterProductPriceHistory (MasterProductID, Price, Place, MerchantID, MerchantName, OffersCount, CityCode, CityID, Rating, Reviews, Preorder, Delivery, DeliveryDays) values (?, ?, ?, ?, ?, ?, ? ,?, ?, ?, ?, ?, ?)",
                        [$masterProductID, $price, $place, $merchantId, $merchantName, $offers, $cityCode, $cityID, $rating, $reviews, $preorder, $delivery, $days]);
                }
                $updates++;
            }

            if ($place < $maxPlace)
            {
                $request->db->do("delete from KaspiMasterProductPriceHistory where MasterProductID=? and Place>?", [$masterProductID, $place]);
            }
        }

        return $updates;
    }

    private function getCommissionWithNDS($comm): float
    {
        if (abs($comm - 6.4) < 0.01)
            return 7.3;
        if (abs($comm - 10.9) < 0.01)
            return 12.5;
        if (abs($comm - 13.5) < 0.01)
            return 15.5;
        return round($comm * 1.14, 1);
    }
}