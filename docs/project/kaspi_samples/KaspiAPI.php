<?php
declare(strict_types=1);

namespace ProfitBot\Modules\ExternalApi;

use ProfitBot\Modules\Http\MobileRequest;
use ProfitBot\Modules\Http\Request;

class KaspiAPI
{
    protected Request|MobileRequest $request;
    private string $apiUrl = 'https://kaspi.kz/shop/api/v2/';
    protected int|null $shopID = null;
    protected int $userID;
    private string $token;

    protected string $errorMessage='';
    protected string|null $errorCause='';
    public bool $isTokenInvalidated = false;

    public string $lastResponseHash = '';

    public string $previousOrdersResponseHash = '';
    public string $ordersResponseHash = '';
    public $orderListResponseHashIsTheSame = false;
    public $ordersListArray = [];
    protected ?string $header_MerchantID = '';

    protected $DoNotSendMerchantHeader = null;

    public function __construct($request, $token, $shopID, $userID, $header_MerchantID = null, $DoNotSendMerchantHeader = null)
    {
        $this->request = $request;
        $this->shopID = $shopID;
        $this->userID = $userID;
        $this->token = $token;
        $this->header_MerchantID = $header_MerchantID;
        $this->DoNotSendMerchantHeader = $DoNotSendMerchantHeader;
    }

    public function getErrorMessage(): string
    {
        return $this->errorMessage;
    }

    protected function communicate(string $url, bool $isPost = false, array|null $postData = null): array|null
    {
        if ($this->isTokenInvalidated)
        {
            $this->errorMessage = "Token is invalidated";
            return null;
        }

        $headers = [
            'X-Auth-Token: ' . $this->token,
            'Content-Type: application/json',
        ];

        if (isset($this->header_MerchantID) && $this->DoNotSendMerchantHeader != 1)
        {
            $headers[] = 'X-Merchant-Uid: ' . $this->header_MerchantID;
        }

        $this->previousOrdersResponseHash = '';
        $this->ordersResponseHash = '';
        $this->orderListResponseHashIsTheSame = false;
        $this->ordersListArray = [];

        $startTime = getCurrentMilliseconds();

        $ch = curl_init();
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 10);
        curl_setopt($ch, CURLOPT_TIMEOUT, 140);
        curl_setopt($ch, CURLOPT_URL, $this->apiUrl . $url);
        curl_setopt($ch, CURLOPT_HTTPHEADER, $headers);
        curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, 0);
        curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, 0);
        $parameters = '';
        if ($isPost)
        {
            curl_setopt($ch, CURLOPT_POST, true);
            $parameters = json_encode($postData);
            curl_setopt($ch, CURLOPT_POSTFIELDS, $parameters);
        }

        $response = curl_exec($ch);
        $error    = curl_error($ch);
        $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        $duration = getDiffCurrentTime($startTime);

        $this->request->db->do("insert into KaspiAPICalls (UserID, ShopID, Method, ResponseCode, Duration, Parameters, Response) values (?, ?, ?, ?, ?, ?, ?)", [$this->userID, isset($this->shopID) && $this->shopID>0 ? $this->shopID : null, $url, $code, $duration, $parameters, '' /*$response*/ ]);

        $obj = json_decode($response, true);
        if (!isset($obj))
        {
            $this->errorMessage = "Cannot parse response from API";
            return null;
        }

        if (str_contains( $response, '"waybill"'))
        {
            $this->lastResponseHash = sha1( stripBetween($response, '"waybill":"', '"') );
        }
        else
        {
            $this->lastResponseHash = sha1($response);
        }

        if (isset($this->shopID) && $this->shopID>0 && str_starts_with($url, 'orders?page'))
        {
            if (isset($obj['data']))
            {
                $responseNoWayBills = stripBetween($response, '"waybill":"', '"');
                $this->ordersListArray = getStringArrayBetweenBrackets($responseNoWayBills, '{"data":', ']', '{', '}');
                $respHash = sha1($responseNoWayBills);
                $urlHash = sha1($url);
                $prevData = $this->request->db->select_row_array("select TOP 1 ResponseHash from KaspiAPICallsResponseHashes where UserID=? and ShopID=? and URLHash=? order by ID DESC", [$this->userID, $this->shopID, $urlHash]);
                if (isset($prevData[0]['ResponseHash']))
                {
                    $this->orderListResponseHashIsTheSame = $prevData[0]['ResponseHash'] === $respHash;
                }
                if (!$this->orderListResponseHashIsTheSame)
                {
                    $this->request->db->do("insert into KaspiAPICallsResponseHashes (UserID, ShopID, URL, URLHash, ResponseHash) values (?, ?, ?, ?, ?)", [$this->userID, $this->shopID, $url, $urlHash, $respHash]);
                }
            }
        }

        if (isset($obj['message']))
        {
            $this->errorMessage = $obj['message'];
            $this->errorCause = $obj['cause'];
            if ($this->errorCause === "NOT AUTHENTICATED")
            {
//                $this->request->db->do("update UserKaspiShops set IsTokenValid=0 where ID=?", [$this->shopID]);
//                $this->request->insertUserMessage(6, [ 'UserID' => $this->userID, 'ShopID' => isset($this->shopID) && $this->shopID>0 ? $this->shopID : null ]);

//                sleep(2);

                $this->isTokenInvalidated = true;
            }
            return null;
        }

        if (isset($obj['errors']))
        {
            $this->errorMessage = $obj['errors'][0]['title'];
            $this->errorCause = $obj['errors'][0]['title'];
            return null;
        }

        return $obj;
    }

    public function getOrdersByStatusAndCreateDate($page, $onpage, $status, $dateFrom, $dateTo)
    {
        return $this->communicate("orders?page[number]={$page}&page[size]={$onpage}&filter[orders][state]={$status}&filter[orders][creationDate][\$ge]={$dateFrom}&filter[orders][creationDate][\$le]={$dateTo}");
    }

    public function getOrderByID($id)
    {
        return $this->communicate("orders/{$id}");
    }

    public function getItemsInOrderByOrderID($id)
    {
        return $this->communicate("orders/{$id}/entries");
    }

    public function getItemInOrderByEntryID($id)
    {
        return $this->communicate("orderentries/{$id}");
    }

    public function getMasterProductByID($id)
    {
        return $this->communicate("masterproducts/{$id}");
    }

    public function getMerchantProductByMasterID($id)
    {
        return $this->communicate("masterproducts/{$id}/merchantProduct");
    }

    public function acceptOrder($id, $code)
    {
        return $this->communicate("orders", true, [
            "data" => [
                "type" => "orders",
                "id" => $id,
                "attributes" => [
                    "code" => $code,
                    "status" => "ACCEPTED_BY_MERCHANT"
                ]
            ]
        ]);
    }

    public function validateToken()
    {
        $data = $this->getOrdersByStatusAndCreateDate(0, 20, 'NEW', time()*1000, (time()+86400)*1000);
        return isset($data);
    }
}