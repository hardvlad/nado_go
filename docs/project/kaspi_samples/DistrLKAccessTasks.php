<?php

namespace ProfitBot\Modules\Services\Methods;

use ProfitBot\Modules\ExternalApi\DumpingManager;
use ProfitBot\Modules\ExternalApi\KaspiImport;
use ProfitBot\Modules\ExternalApi\KaspiMarketingManager;
use ProfitBot\Modules\ExternalApi\TasksManagement;
use ProfitBot\Modules\ExternalApi\WhatsappOrderMessage;
use ProfitBot\Modules\Services\ServiceAbstraction;

class DistrLKAccessTasks extends ServiceAbstraction
{
    public function perform()
    {
        $tm = new TasksManagement($this->request);

        $action = $this->request->getParameter('action') ?? '';
        $agentID = $this->request->getParameter('id');

        $this->request->db->do("update InfraJobRunners set LastDate=GetDate() where Code=?", [$agentID]);

        if ($action === 'getTasks')
        {
            $this->result['tasks'] = $tm->getFirstTaskOfTypeToDoAndSetStarted(3);
        }
        elseif ($action === 'getMarketingTasks')
        {
            $this->result['tasks'] = $tm->getFirstTaskOfTypeToDoAndSetStarted(5);
        }
        elseif ($action === 'getTestAllHeaders')
        {
            $this->result['tasks'] = getallheaders();
        }
        elseif ($action === 'getMarketingFirstImportTasks')
        {
            $this->result['tasks'] = $this->request->db->select_row_array("select TOP 1 ID,MarketingLogin,MarketingPassword,KaspiMerchantID from UserKaspiShops where UseMarketingAccess=1 and MarketingFirstImportFinished IS NULL and MarketingFirstImportStartDate IS NULL");
            foreach ($this->result['tasks'] as $task)
            {
                $this->request->db->do("UPDATE UserKaspiShops SET MarketingFirstImportStartDate=GETDATE() WHERE ID=?", [ $task['ID'] ]);
            }
        }
        elseif ($action === 'marketingFirstImportFinished')
        {
            $data = GetMobileData();
            $shopID = $data['shopID'];
            $this->request->db->do("UPDATE UserKaspiShops SET MarketingFirstImportFinished=1, MarketingFirstImportEndDate=GETDATE(), MarketingLastUpdated=GetDate() WHERE ID=?", [ $shopID ]);
        }
        elseif ($action === 'getMarketingPeriodicImportTasks')
        {
            $this->result['tasks'] = $this->request->db->select_row_array("select TOP 1 ID,MarketingLogin,MarketingPassword,KaspiMerchantID from UserKaspiShops where UseMarketingAccess=1 and MarketingFirstImportFinished=1 and MarketingLastUpdated<=DateAdd(minute, -120, GetDate()) order by MarketingLastUpdated");
            foreach ($this->result['tasks'] as $task)
            {
                $this->request->db->do("UPDATE UserKaspiShops SET MarketingLastUpdated=GETDATE() WHERE ID=?", [ $task['ID'] ]);
            }
        }
        elseif ($action === 'marketingPeriodicImportFinished')
        {
            $data = GetMobileData();
            $shopID = $data['shopID'];
            $this->request->db->do("UPDATE UserKaspiShops SET MarketingLastUpdated=GetDate() WHERE ID=?", [ $shopID ]);
        }
        elseif ($action === 'getPriceDumpingTasks')
        {
            $startTime_ts = microtime(true);
            $dumpingMutex = new \SyncMutex("sync_get_dumping_mutex");
            while (!$dumpingMutex->lock(15000))
            {
                usleep(10000);
                $this->request->SendMessageToTelegram("Lock sync get dumping mutex failed", $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            $toMutexTime_ts = microtime(true) - $startTime_ts;

            $getSqlTime_ts = 0;

            try
            {
                $this->result['tasks'] = $this->request->db->select_row_array("
with AllowedShops as (select ShopID, 
(
    select count(distinct c.ID) from UserDumpingCommands c 
    inner join KaspiMerchantProducts p on c.ProductID=p.ID 
    where c.AssignedToExecuteDate IS NOT NULL and AssignedToExecuteDate>DATEADD(MINUTE,-31,GetDate()) and p.ShopID=R.ShopID
) as cnt 
from 
(
select distinct pp.ShopID
from UserDumpingCommands cp  
inner join KaspiMerchantProducts pp on pp.ID=cp.ProductID 
where AssignedToExecuteDate IS NULL
) as R
)
select top 5 c.ID,p.ParentMerchantID,p.Code,s.LKEmail,s.LKPassword, s.KaspiMerchantID,p.ShopID,c.Price,c.ProductID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=p.ShopID and IsActive = 1 order by ID DESC) as AccessData 
from UserDumpingCommands c 
inner join KaspiMerchantProducts p on c.ProductID=p.ID 
inner join UserKaspiShops s on s.ID=p.ShopID where AssignedToExecuteDate IS NULL
and p.ShopID in (
    select ShopID from AllowedShops where cnt<250
)
order by c.Date
");

                $getSqlTime_ts = microtime(true) - $startTime_ts;

                $arr = [];
                foreach ($this->result['tasks'] as $task)
                {
                    $this->request->db->do("UPDATE UserDumpingCommands SET AssignedToExecuteDate=GETDATE() WHERE ID=?", [ $task['ID'] ]);
                    $preOrderData= $this->request->db->select_row_array("select R.Code,
IIF(PreOrderIncomeDateDays IS NOT NULL and PreOrderIncomeDateDays>0, PreOrderIncomeDateDays, PreOrderDays) as PreOrderDays
from
(
    select s.Code,po.PreOrderDays,po.PreOrderIncomeDate,DateDiff(DAY,GetDate(),po.PreOrderIncomeDate) as PreOrderIncomeDateDays from KaspiMerchantProductStorePreOrder po 
    inner join UserKaspiShopsStores s on s.ID=po.StoreID
    where po.ProductID=? and s.DeletedDate IS NULL
) as R", [ $task['ParentMerchantID'] ?? $task['ProductID'] ]);

                    if (isset($preOrderData) && count($preOrderData) > 0)
                    {
                        $task['PreOrderData'] = $preOrderData;
                    }

                    if (isset($task['AccessData']))
                    {
                        $task['AccessData'] = json_decode($task['AccessData'], true);
                    }

                    $arr[] = $task;
                }

                $this->result['tasks'] = $arr;
            }
            catch (\Throwable $e)
            {
                $this->request->SendMessageToTelegram("Getting list of dumping tasks failed \n" . $e->getMessage(), $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            $finishTime_ts = microtime(true) - $startTime_ts;

            $dumpingMutex->unlock();

            $finishUnlockTime_ts = microtime(true) - $startTime_ts;

//            $this->request->InsertToLog("getPriceDumpingTasks - mutex lock time: $toMutexTime_ts, get sql time: $getSqlTime_ts, finish time: $finishTime_ts, finish unlock time: $finishUnlockTime_ts");

        }
        elseif ($action === 'postPriceDumpingTasksResult')
        {
            $data = GetMobileData();
            $ID = $data['ID'];
            $result = $data['result'];
            $data = $this->request->db->select_row_array("select ID,ProductID,Price from UserDumpingCommands where ID=?", [ $ID ]);
            foreach ($data as $task)
            {
                $this->request->db->do("UPDATE UserDumpingCommands SET ExecutedDate=GETDATE() WHERE ID=?", [ $task['ID'] ]);
                if (isset($result['id']))
                {
                    $this->request->db->do("UPDATE KaspiMerchantProducts SET Price=? WHERE ID=?", [ $task['Price'], $task['ProductID'] ]);
//                    $this->request->db->do("Insert into UserDumpingLoadTracker (CommandID, FileID) values (?, ?)", [ $task['ID'], $result['id'] ]);
                }
            }
        }
        elseif ($action === 'getShopsFinishDate')
        {
            $sql = "select R.*,LKEmail,LKPassword,KaspiMerchantID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=s.ID and IsActive = 1 order by ID DESC) as AccessData,
case when OrdersCount>200 then NULL else (SELECT ( ',' + p.Code)
                           FROM KaspiOrders p
                           WHERE ShopID=R.ShopID and ((ActualDeliveryDateTS IS NULL and p.StatusID=3) or (ActualReturnDateTS IS NULL and p.StatusID=7))
                           FOR XML PATH( '' )
                          ) 
end as OrdersList
from (
select min(CreationDateTS) as MinDate,max(CreationDateTS) as MaxDate,count(o.ID) as OrdersCount,ShopID from KaspiOrders o
inner join UserKaspiShops s on s.ID=o.ShopID
where s.DeletedDate IS NULL and s.UseLKAccess=1 and ShopID!=24
and ((ActualDeliveryDateTS IS NULL and o.StatusID=3) or (ActualReturnDateTS IS NULL and o.StatusID=7))
and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=s.UserID and us.IsActive=1)
group by ShopID
) as R
inner join UserKaspiShops s on s.ID=R.ShopID
order by ShopID
";
            $data = $this->request->db->select_row_array($sql);
            $arr = [];
            foreach ($data as $task)
            {
                if (isset($task['AccessData']))
                    $task['AccessData'] = json_decode($task['AccessData'], true);
                $arr[] = $task;
            }

            $this->result['shops'] = $arr;
        }
        elseif ($action === 'postResult')
        {
            $data = GetMobileData();
            $taskID = $data['taskID'];
            $result = $data['result'];

            $this->request->db->do("UPDATE ScheduledTasks SET FinishedDate = GETDATE(), DistrResult=? WHERE ID = ?", [ json_encode($result), $taskID ]);
        }
        elseif ($action === "postShopsFinishDate")
        {
            $data = GetMobileData();
            $shopID = $data['shopID'];
            $result = $data['result'];

            foreach ($result as $orderData)
            {
                $code = $orderData[0] ?? null;
                $date = $orderData[1] ?? null;
                if ($code && $date)
                {
                    $this->request->db->do("UPDATE KaspiOrders SET ActualDeliveryDateTS = ?, ActualDeliveryDate = ? WHERE ShopID = ? AND Code = ?", [ msTimeToInt($date), msTimeToSQL($date), $shopID, $code ]);
                }
            }
        }
        elseif ($action === "postShopsFinishReturnDate")
        {
            $data = GetMobileData();
            $shopID = $data['shopID'];
            $result = $data['result'];

            foreach ($result as $orderData)
            {
                $code           = $orderData[0] ?? null;
                $dateCompleted  = $orderData[1] ?? null;
                $dateReturned   = $orderData[2] ?? null;

                if ($dateReturned)
                {
                    $dateReturned += 2*3600;
                }

                $this->request->db->do("UPDATE KaspiOrders SET ActualDeliveryDateTS = ?, ActualDeliveryDate = ?, ActualReturnDateTS = ?, ActualReturnDate = ? WHERE ShopID = ? AND Code = ?",
                    [   $dateCompleted, isset($dateCompleted) ? msTimeToSQL($dateCompleted*1000): null,
                        $dateReturned, isset($dateReturned) ? msTimeToSQL($dateReturned*1000) : null, $shopID, $code
                    ]);

            }
        }
        elseif ($action === 'getProductsToCheckPrice')
        {
            $mutex = new \SyncMutex("sync_price_jobs_mutex");
            while (!$mutex->lock(100000))
            {
                usleep(10000);
                $this->request->SendMessageToTelegram("Lock sync_price_jobs_mutex failed", $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            try
            {
                $items = $this->request->db->select_row_array("
select top 200 * from (
select distinct m.ID,m.Code, m.LastPriceCheckDate,
(
    SELECT ( ',' + Cities.Code) from 
    (
        select distinct c.Code
        FROM KaspiCities c
        WHERE c.Name in (select distinct st.CityName from UserKaspiShopsStores st 
        where st.ShopID IN 
        (
            select distinct p.ShopID from KaspiMerchantProducts p 
            inner join UserKaspiShops s on s.ID=p.ShopID 
            inner join Users u on u.ID=s.UserID
            where p.KaspiMasterProductID=m.ID and s.DeletedDate IS NULL and u.DeletedDate IS NULL and p.IsAvailable=1 and p.IsDumpingOn=1
            and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=s.UserID and us.IsActive=1)
        ) and st.DeletedDate IS NULL )
    ) as Cities
    FOR XML PATH( '' )
) as CityCode
from KaspiMasterProducts m
where 
exists 
(
    select p.ID from KaspiMerchantProducts p 
    inner join UserKaspiShops s on s.ID=p.ShopID 
    inner join Users u on u.ID=s.UserID
    where p.KaspiMasterProductID=m.ID and s.DeletedDate IS NULL and u.DeletedDate IS NULL and p.IsAvailable=1 and p.IsDumpingOn=1
    and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=s.UserID and us.IsActive=1)
) 
)
as R
order by LastPriceCheckDate
");

                $arr = [];
                foreach ($items as $item)
                {
                    $this->request->db->do("update KaspiMasterProducts set LastPriceCheckDate=GetDate() where ID=?", [$item['ID']]);
                    $cities = $item['CityCode'];
                    if (str_starts_with($cities, ','))
                    {
                        $cities = substr($cities, 1);
                    }
                    $arr[] = [ 'Code' => $item['Code'], 'Cities' => explode(',', $cities) ];
                }
                $this->result['data'] = $arr;
            }
            catch (\Throwable $e){}

            $mutex->unlock();
        }
        elseif ($action === 'saveProductsPrices')
        {
            $dumper = new DumpingManager($this->request);

            $data = GetMobileData();
//            $this->request->InsertToLog("saveProductsPrices - Получен результат: " . json_encode($data));
            foreach ($data as $item)
            {
                $noDumping = isset($item['noDumping']) && $item['noDumping'];
                $updates = KaspiImport::setMasterProductPrices($this->request, $item['Code'], $item['cityOffers'], $noDumping);
                if (!$noDumping)
                {
                    $dumper->checkAndProcessDumpingForMasterProduct($item['Code'], $item['cityOffers'], $updates);
                }
            }
        }
        elseif ($action === 'postMarketingDayResult')
        {
            $data = GetMobileData();
            new KaspiMarketingManager($this->request)->saveMarketingDayResult($data);
        }
        elseif ($action === 'getFirstShopItemsImportTasks')
        {
            $mutex = new \SyncMutex("sync_first_items_jobs_mutex");
            while (!$mutex->lock(100000))
            {
                usleep(10000);
                $this->request->SendMessageToTelegram("Lock sync_first_items_jobs_mutex failed", $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            $data = $this->request->db->select_row_array("SELECT TOP 1 t.ID,t.UserID,t.LKEmail,t.LKPassword, t.KaspiMerchantID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=t.ID and IsActive = 1 order by ID DESC) as AccessData 
FROM UserKaspiShops t 
where t.LKEmail IS NOT NULL and t.LKPassword IS NOT NULL and t.DeletedDate IS NULL and t.UseLKAccess IS NULL and t.LKItemsDownloadedDate IS NULL 
order by t.ID
");
            $arr = [];
            foreach ($data as $task)
            {
                $this->request->db->do("update UserKaspiShops set LKItemsDownloadedDate=GetDate() where ID = ?", [ $task['ID'] ]);
                if (isset($task['AccessData']))
                {
                    $task['AccessData'] = json_decode($task['AccessData'], true);
                }
                $arr[] = $task;
            }
            $this->result['tasks'] = $arr;

            $mutex->unlock();
        }
        elseif ($action === 'getPeriodicShopItemsImportTasks')
        {
            $mutex = new \SyncMutex("sync_periodic_items_jobs_mutex");
            while (!$mutex->lock(100000))
            {
                usleep(10000);
                $this->request->SendMessageToTelegram("Lock sync_periodic_items_jobs_mutex failed", $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            $data = $this->request->db->select_row_array("
SELECT TOP 1 t.ID,t.UserID,t.LKEmail,t.LKPassword, t.KaspiMerchantID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=t.ID and IsActive = 1 order by ID DESC) as AccessData 
FROM UserKaspiShops t 
where t.LKEmail IS NOT NULL and t.LKPassword IS NOT NULL and t.DeletedDate IS NULL 
and t.FirstImportFinished=1 and (t.UseLKAccess=1 and t.LKItemsDownloadedDate < DATEADD(minute, -15, GETDATE()))
and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=t.UserID and us.IsActive=1)
order by t.LKItemsDownloadedDate
");
            $arr = [];
            foreach ($data as $task)
            {
                $this->request->db->do("update UserKaspiShops set LKAccessDataRenewDate=GetDate(), LKItemsDownloadedDate=GetDate() where ID = ?", [ $task['ID'] ]);
                if (isset($task['AccessData']))
                {
                    $task['AccessData'] = json_decode($task['AccessData'], true);
                }
                $arr[] = $task;
            }
            $this->result['tasks'] = $arr;

            $mutex->unlock();
        }
        elseif ($action === 'getRequestedItemsLoaderTasks')
        {
            $mutex = new \SyncMutex("sync_requested_items_jobs_mutex");
            while (!$mutex->lock(100000))
            {
                usleep(10000);
                $this->request->SendMessageToTelegram("Lock sync_requested_items_jobs_mutex failed", $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            $data = $this->request->db->select_row_array("SELECT TOP 1 t.ID,t.UserID,t.LKEmail,t.LKPassword, t.KaspiMerchantID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=t.ID and IsActive = 1 order by ID DESC) as AccessData 
FROM UserKaspiShops t 
where t.LKEmail IS NOT NULL and t.LKPassword IS NOT NULL and t.DeletedDate IS NULL and GoodsLoadRequested=1 and GoodsLoadAssignedDate IS NULL 
order by t.GoodsLoadRequestDate");

            $arr = [];
            foreach ($data as $task)
            {
                $this->request->db->do("update UserKaspiShops set GoodsLoadAssignedDate=GetDate() where ID = ?", [ $task['ID'] ]);
                if (isset($task['AccessData']))
                {
                    $task['AccessData'] = json_decode($task['AccessData'], true);
                }
                $arr[] = $task;
            }
            $this->result['tasks'] = $arr;

            $mutex->unlock();
        }
        elseif ($action === 'postShopItemsDataRequestedQuant')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $userID = $data['UserID'];
            $this->request->db->do("update UserKaspiShops set GoodsLoadTotalItems=?, GoodsLoadLoadedItems=0 where ID = ? and UserID=?", [ $data['totalQuant'], $shopID, $userID ]);
        }
        elseif ($action === 'postShopItemsDataRequested')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $userID = $data['UserID'];
            $tc = new KaspiImport( $this->request, '', $shopID, $userID );
            $tc->updateShopItemsFromPersonalCabinetData( $data['result'], $data['afterRegistration'] );
            $this->request->db->do("update UserKaspiShops set GoodsLoadLoadedItems=GoodsLoadLoadedItems+? where ID = ? and UserID=?", [ count($data['result']), $shopID, $userID ]);
        }
        elseif ($action === 'postShopItemsImportResultRequested')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $userID = $data['UserID'];
            $this->request->db->do("update UserKaspiShops set GoodsLoadFinishedDate=GetDate(), GoodsLoadSuccess=?,GoodsLoadRequested=0 where ID = ? and UserID=?", [ $data['success'] ? 1 : 0, $shopID, $userID ]);
        }
        elseif ($action === 'postShopItemsImportResult')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $credsValid = $data['credsValid'];
            $creds = $data['creds'];
            $this->request->db->do("update UserKaspiShops set UseLKAccess=?, LKAccessDataRenewDate=GETDATE(), LKAccessActive=?, LKAccessData=?, LKItemsDownloadedDate=GetDate() where ID = ?", [ $credsValid ? 1 : 0, $credsValid ? 1 : 0, json_encode($creds), $shopID ]);
        }
        elseif ($action === 'postShopStoresData')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $userID = $data['UserID'];
            $tc = new KaspiImport( $this->request, '', $shopID, $userID );
            $tc->updateShopStoresFromPersonalCabinetData( $data['result'] );
        }
        elseif ($action === 'postShopItemsData')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $userID = $data['UserID'];
            $tc = new KaspiImport( $this->request, '', $shopID, $userID );
//            $this->request->InsertToLog("postShopItemsData - Получен результат для магазина $shopID: " . json_encode($data));

            $tc->updateShopItemsFromPersonalCabinetData( $data['result'], $data['afterRegistration'] );
        }
        elseif ($action === 'getLKOTPSenderTasks')
        {
            $data = $this->request->db->select_row_array("SELECT TOP 1 ID,Phone FROM UserKaspiShopsOTPRegistration where BotAssigned IS NULL");
            foreach ($data as $task)
            {
                $this->request->db->do("update UserKaspiShopsOTPRegistration set BotAssigned=? where ID = ?", [ $agentID, $task['ID'] ]);
            }
            $this->result['tasks'] = $data;
        }
        elseif ($action === 'postOTPSenderResult')
        {
            $data = GetMobileData();
            $ID = $data['ID'];
            $RequestData = $data['RequestData'];
            $OTPSendResult = $data['OTPSendResult'];
            $this->request->db->do("update UserKaspiShopsOTPRegistration set RequestSentDate=GETDATE(), RequestData=?, OTPSendResult=? where ID = ?", [ json_encode($RequestData), $OTPSendResult, $ID ]);
        }
        elseif ($action === 'getLKOTPCheckerTasks')
        {
            $data = $this->request->db->select_row_array("SELECT TOP 1 ID,RequestData,OTPCode,Name,Email FROM UserKaspiShopsOTPRegistration where OTPSendResult=1 and BotAssigned=? and OTPCode is not null and CreateStartedDate IS NULL", [ $agentID ]);
            foreach ($data as $task)
            {
                $this->request->db->do("update UserKaspiShopsOTPRegistration set CreateStartedDate=GetDate() where ID = ?", [ $task['ID'] ]);
            }
            $this->result['tasks'] = $data;
        }
        elseif ($action === 'postOTPCheckerResult')
        {
            $data = GetMobileData();
            $ID = $data['ID'];
            $UserCreateResult = $data['UserCreateResult'];
            $token = $data['token'] ?? null;
            $result = $data['result'] ?? null;
            if ($result['needSelectMerchant'] ?? false)
            {
                $this->request->db->do("update UserKaspiShopsOTPRegistration set ConfirmedDate=GETDATE(), UserCreateResult=?, ResultData=?, MerchantsData=? where ID = ?", [ $UserCreateResult, json_encode($result), json_encode($result['merchants']), $ID ]);
            }
            else
            {
                $this->request->db->do("update UserKaspiShopsOTPRegistration set ConfirmedDate=GETDATE(), UserCreateResult=?, APIToken=? where ID = ?", [ $UserCreateResult, $token, $ID ]);
            }
        }
        elseif ($action === 'getNotSetProductImagesTasks')
        {
            $data = $this->request->db->select_row_array("select top 100 ID,Code from KaspiMasterProducts where FirstImage IS NULL");
            $this->result['tasks'] = $data;
        }
        elseif ($action === 'setNotSetProductImages')
        {
            $data = GetMobileData();
            foreach ($data as $item)
            {
                $result = $item['result'];
                $brand = $item['brand'];
                if (isset($result) && is_array($result) && count($result) > 0)
                {
                    $this->request->db->do('update KaspiMasterProducts set FirstImage =?, Images=? where ID=?', [$result[0], implode(',', $result), $item['ID']]);
                }
                if (isset($brand))
                {
                    $this->request->db->do('update KaspiMasterProducts set Brand =? where ID=?', [$brand, $item['ID']]);
                }
            }
        }
        elseif ($action === 'getClientsPhonesToParse')
        {
            $data = $this->request->db->select_row_array("select top 1000 o.ID,o.ShopID,o.Code,o.CustomerID,c.Phone,c.KaspiID,s.LKEmail,s.LKPassword, s.KaspiMerchantID from KaspiOrders o
inner join KaspiCustomers c on c.ID=o.CustomerID
inner join UserKaspiShops s on s.ID=o.ShopID
where c.Phone='+0(000)-000-00-00' and LKEmail IS NOT NULL
and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where us.UserID=s.UserID and us.IsActive=1)
order by CustomerID,o.Code DESC");
            $this->result['tasks'] = $data;
        }
        elseif ($action === 'postClientsPhonesParsed')
        {
            $data = GetMobileData();
            foreach ($data as $item)
            {
                $customerID = $item['CustomerID'];
                $phone = $item['Phone'];
                $this->request->db->do("update KaspiCustomers set Phone=? where ID=?", [ $phone, $customerID ]);
                $ordersData = $this->request->db->select_row_array("select u.ID,u.OrderID,u.JustCreated,o.ShopID,s.UserID,u.StatusID from UserWhatsappOrderProcessAfterPhoneUpdate u inner join KaspiOrders o on o.ID=u.OrderID inner join UserKaspiShops s on s.ID=o.ShopID where u.PhoneUpdateDate IS NULL and o.CustomerID=?", [ $customerID ]);
                foreach ($ordersData as $order)
                {
                    $this->request->db->do("update UserWhatsappOrderProcessAfterPhoneUpdate set PhoneUpdateDate=GetDate() where ID=?", [ $order['ID'] ]);
                    $wo = new WhatsappOrderMessage($this->request, $order['UserID'], $order['ShopID']);
                    $wo->processOrderFromImport($order['OrderID'], $order['JustCreated'] == 1, $order['StatusID']);
                }
            }
        }
        elseif( $action === 'lkGetSendPriceFileTasks' )
        {
            $data = $this->request->db->select_row_array("
SELECT TOP 1 t.ID,t.UserID,t.LKEmail,t.LKPassword,t.KaspiDownloadCode,t.KaspiMerchantID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=t.ID and IsActive = 1 order by ID DESC) as AccessData
FROM UserKaspiShops t 
where t.LKEmail IS NOT NULL and t.LKPassword IS NOT NULL and t.DeletedDate IS NULL and t.UseLKAccess=1 
  and t.SendKaspiPriceRequested=1 and t.SendKaspiPriceAssignedDate IS NULL
  and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=t.UserID and us.IsActive=1)
");
            if (!isset($data) || count($data) === 0)
            {
                $data = $this->request->db->select_row_array("
SELECT TOP 1 t.ID,t.UserID,t.LKEmail,t.LKPassword,t.KaspiDownloadCode,t.KaspiMerchantID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=t.ID and IsActive = 1 order by ID DESC) as AccessData 
FROM UserKaspiShops t 
where t.LKEmail IS NOT NULL and t.LKPassword IS NOT NULL and t.DeletedDate IS NULL and t.UseLKAccess=1 
  and t.DoSendKaspiPrice=1 and t.LastKaspiPriceSendDate < DATEADD(minute, -16, GETDATE()) and SendKaspiPriceAssignedDate IS NULL 
  and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=t.UserID and us.IsActive=1)
order by t.LastKaspiPriceSendDate
");
            }
            if (isset($data) && count($data) > 0)
            {
                $this->request->db->do("update UserKaspiShops set LastKaspiPriceSendDate=GetDate(), SendKaspiPriceAssignedDate=GetDate() where ID = ?", [ $data[0]['ID'] ]);
                $this->request->db->do("insert into UserKaspiShopsUploadPriceTasks (ShopID) values (?)", [ $data[0]['ID'] ]);

                if (isset($data[0]['AccessData']))
                {
                    $data[0]['AccessData'] = json_decode($data[0]['AccessData'], true);
                }

                $data[0]['LoadID'] = $this->request->db->GetLastID();
            }
            $this->result['tasks'] = $data;
        }
        elseif( $action === 'lkPostSendPriceFileResult' )
        {
            $data = GetMobileData();
            $this->request->db->do("update UserKaspiShops set LastKaspiPriceSendDate=GetDate(), SendKaspiPriceAssignedDate=NULL,SendKaspiPriceFinishedDate=GetDate(),SendKaspiPriceSuccess=?,SendKaspiPriceRequested=0 where ID = ?", [ isset($data['result']['id']) ? 1 : 0, $data['ID'] ]);
            $this->request->db->do("update UserKaspiShopsUploadPriceTasks set FinishedDate=GetDate(), PriceID=? where ID = ?", [ $data['result']['id'] ?? null, $data['LoadID'] ]);
        }
        elseif( $action === 'postShopItemsForSync' )
        {
            $data = GetMobileData();
            $shopID = $data['ID'];

            $codes = array_merge($data['active'], $data['inactive']);
            $itemsData = $this->request->db->select_row_array("select distinct ID, Code, IsAvailable from KaspiMerchantProducts where ShopID=?", [ $shopID ]);
            $count = 0;
            $activatedCount = 0;
            $deactivatedCount = 0;
            foreach ($itemsData as $item)
            {
                if (!in_array($item['Code'], $codes))
                {
                    if (isset($item['IsAvailable']))
                    {
                        $this->request->db->do("update KaspiMerchantProducts set IsAvailable=NULL where ID=? and ShopID=?", [ $item['ID'], $shopID ]);
                        $this->request->db->do(" insert into KaspiMerchantProductsItemUnavailable (ItemID) values (?)", [ $item['ID'] ]);
                        $count++;
                    }
                }
                elseif (in_array($item['Code'], $data['active']))
                {
                    if ($item['IsAvailable'] != 1)
                    {
                        $this->request->db->do("update KaspiMerchantProducts set IsAvailable=1 where ID=? and ShopID=?", [ $item['ID'], $shopID ]);
                        $activatedCount++;
                    }
                }
                elseif (in_array($item['Code'], $data['inactive']))
                {
                    if ($item['IsAvailable'] != 0)
                    {
                        $this->request->db->do("update KaspiMerchantProducts set IsAvailable=0 where ID=? and ShopID=?", [$item['ID'], $shopID]);
                        $deactivatedCount++;
                    }
                }
            }

            $this->request->db->do("insert into KaspiMerchantProductsSyncItemsData(ShopID, CodesCount, ItemsDisabled, ActiveItemsCount, InActiveItemsCount, TotalCount, ItemsActivated, ItemsDeactivated) values (?,?,?,?,?,?,?,?)",
                [ $shopID, count($itemsData ?? []), $count,
                    count($data['active'] ?? []), count($data['inactive'] ?? []), count($data['active'] ?? []) + count($data['inactive'] ?? []),
                    $activatedCount, $deactivatedCount
                ]);

        }
        elseif ($action === 'getNoDumpingProductsToCheckPrice')
        {
            $mutex = new \SyncMutex("sync_no_dumping_price_jobs_mutex");
            while (!$mutex->lock(100000))
            {
                usleep(10000);
                $this->request->SendMessageToTelegram("Lock sync_no_dumping_price_jobs_mutex failed", $_ENV['TELEGRAM_CHAT_JOBS']);
            }

            try
            {
                $items = $this->request->db->select_row_array("
select top 200 * from (
select distinct m.ID,m.Code, m.LastPriceNoDumpingCheckDate,
(
    SELECT ( ',' + Cities.Code) from 
    (
        select distinct c.Code
        FROM KaspiCities c
        WHERE c.Name in (select distinct st.CityName from UserKaspiShopsStores st 
        where st.ShopID IN 
        (
            select distinct p.ShopID from KaspiMerchantProducts p 
            inner join UserKaspiShops s on s.ID=p.ShopID 
            inner join Users u on u.ID=s.UserID
            where p.KaspiMasterProductID=m.ID and s.DeletedDate IS NULL and u.DeletedDate IS NULL and p.IsAvailable=1 and p.IsDumpingOn=1
            and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=s.UserID and us.IsActive=1)
        ) and st.DeletedDate IS NULL )
    ) as Cities
    FOR XML PATH( '' )
) as CityCode
from KaspiMasterProducts m
where 
exists 
(
    select p.ID from KaspiMerchantProducts p 
    inner join UserKaspiShops s on s.ID=p.ShopID 
    inner join Users u on u.ID=s.UserID
    where p.KaspiMasterProductID=m.ID and s.DeletedDate IS NULL and u.DeletedDate IS NULL and (p.IsDumpingOn=0 or p.IsDumpingOn IS NULL) and p.IsAvailable = 0
    and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=s.UserID and us.IsActive=1)
) 
)
as R
order by LastPriceNoDumpingCheckDate
");

                $arr = [];
                foreach ($items as $item)
                {
                    $this->request->db->do("update KaspiMasterProducts set LastPriceNoDumpingCheckDate=GetDate() where ID=?", [$item['ID']]);
                    $cities = $item['CityCode'];
                    if (str_starts_with($cities, ','))
                    {
                        $cities = substr($cities, 1);
                    }
                    $arr[] = [ 'Code' => $item['Code'], 'Cities' => explode(',', $cities) ];
                }
                $this->result['data'] = $arr;
            }
            catch (\Throwable $e){}

            $mutex->unlock();
        }
        elseif( $action === 'getEmailOtpCode' )
        {
            $email = $this->request->getParameter('email');
            $otpCode = $this->request->db->select_row_array("select top 1 ID, Code from KaspiEmailOTPCodes where Email=? and UsedDate IS NULL order by ID desc", [ $email ]);
            if (isset($otpCode) && count($otpCode) > 0)
            {
                $this->request->db->do("update KaspiEmailOTPCodes set UsedDate=GetDate() where ID=?", [ $otpCode[0]['ID'] ]);
            }
            $this->result['otpCode'] = $otpCode[0]['Code'] ?? null;
        }
        elseif ($action === 'getShopsToGetCredentials')
        {
            $this->result['tasks'] = $this->request->db->select_row_array("select ID, LKEmail, LKPassword, KaspiMerchantID from UserKaspiShops s where LKEmail like 'pbsrv_%' and DeletedDate IS NULL
and exists (select us.ID from UserServices us inner join Services srv on srv.ID=us.ServiceID where srv.Code like 'profitbot%' and us.UserID=s.UserID and us.IsActive=1)
and not exists (select ID from KaspiShopCredentials where ShopID=s.ID and IsActive=1)");
        }
        elseif ($action === 'setShopCredentials')
        {
            $data = GetMobileData();
            $shopID = $data['ID'];
            $creds = json_encode($data['creds']);

            $this->request->db->do("update KaspiShopCredentials set IsActive=0 where ShopID=? and IsActive=1", [ $shopID ]);
            $this->request->db->do("insert into KaspiShopCredentials (ShopID, AccessData, IsActive) values (?, ?, 1)", [ $shopID, $creds ]);
        }
        elseif ($action === 'getShopCredentials')
        {
            $shopID = $this->request->getParameter('ID');
            $data = $this->request->db->select_row_array("select top 1 AccessData from KaspiShopCredentials where ShopID=? and IsActive = 1 order by ID DESC", [ $shopID ]);
            $this->result['data'] = isset($data[0]['AccessData']) ? json_decode($data[0]['AccessData'], true) : null;
        }
        elseif ($action === 'manageShopLock')
        {
            $shopID = $this->request->getParameter('ID');
            $type = $this->request->getParameter('type');
            $lockAction = $this->request->getParameter('lockAction');

            $lock_id = "shop_lock_{$type}_{$shopID}";

            if ($lockAction === 'lock')
            {
                $val = $this->request->db->redis->getValue($lock_id);
                if ($val)
                {
                    $this->result['status'] = "already_locked";
                }
                else
                {
                    $this->request->db->redis->setValue($lock_id, 1, 180);
                    $this->result['status'] = "locked";
                }
            }
            elseif ($lockAction === 'unlock')
            {
                $this->request->db->redis->deleteValue($lock_id);
                $this->result['status'] = "unlocked";
            }
        }
        elseif ($action === 'getFileLoadStatus')
        {
            $data = $this->request->db->select_row_array("select top 50 lt.ID,mp.ShopID,s.LKEmail,s.LKPassword,s.KaspiMerchantID,lt.FileID,
(select top 1 AccessData from KaspiShopCredentials where ShopID=mp.ShopID and IsActive = 1 order by ID DESC) as AccessData 
from UserDumpingLoadTracker lt
inner join UserDumpingCommands udc on udc.ID=lt.CommandID
inner join KaspiMerchantProducts mp on mp.ID=udc.ProductID
inner join UserKaspiShops s on s.ID=mp.ShopID
where CompletedDate is null
order by lt.ID");

            $arr = [];
            foreach ($data as $task)
            {
                if (isset($task['AccessData']))
                {
                    $task['AccessData'] = json_decode($task['AccessData'], true);
                }
                $arr[] = $task;
            }

            $this->result['tasks'] = $arr;
        }
        elseif ($action === 'postFileLoadStatus')
        {
            $data = GetMobileData();
            $trackerID = $data['ID'];
            $this->request->db->do("update UserDumpingLoadTracker set CompletedDate=GETDATE() where ID=?", [ $trackerID ]);
        }

        $this->result['success'] = true;
        $this->result['errorMessage'] = '';
        $this->result['resultCode'] = 200;
    }
}