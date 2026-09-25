<?php

$config = json_decode(file_get_contents(__DIR__ . "/config.json"), true);
$LK_DISTR_ID = $config['LK_DISTR_ID'];
$token = $config['token'];

class KaspiLogin
{
    protected $merchantId;
    protected $ampCookie;
    protected $mc_session_cookie;
    protected $mc_sid_cookie;

    public function sendKaspiRequest($url, $sendHeaders, $data, $doPost = true, $doEncode = true, $uploadFile = false, $fileData = null)
    {
        $ch = curl_init();
        $headers = [];
        $setCookie = '';
        $location = '';
        curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
        curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 30);
        curl_setopt($ch, CURLOPT_TIMEOUT, 50);
        curl_setopt($ch, CURLOPT_URL, $url);
        if ($doPost)
        {
            curl_setopt($ch, CURLOPT_POST, true);
            if ($uploadFile)
            {
                $boundary = '----WebKitFormBoundary' . md5(time());
                $sendHeaders[5] = 'Content-Type: multipart/form-data; boundary=' . $boundary;
                $body = "--" . $boundary . "\r\n";
                $body .= 'Content-Disposition: form-data; name="file"; filename="' . md5(time()) . '.xml' . '"' . "\r\n";
                $body .= 'Content-Type: text/xml' . "\r\n\r\n";
                $body .= $fileData . "\r\n";
                $body .= "--" . $boundary . "--\r\n";
                curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
            }
            else
            {
                curl_setopt($ch, CURLOPT_POSTFIELDS, $doEncode ? json_encode($data) : $data);
            }
        }
        curl_setopt($ch, CURLOPT_HTTPHEADER, $sendHeaders);
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
        return [$response, $headers, $httpCode, $setCookie, $location];
    }

    public function getEmailOtpCode($email, $timeout = 60)
    {
        if (!str_starts_with($email, 'pbsrv_'))
            return null;

        print "Waiting for code $email\n";

        $startTime = time();
        $code = null;
        while (time() - $startTime < $timeout)
        {
            $action = 'getEmailOtpCode';
            $lk_distr_id = $GLOBALS['LK_DISTR_ID'];
            $token = $GLOBALS['token'];

            $d = file_get_contents("https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}&email={$email}");
            $data = json_decode($d, true);
            if (isset($data['otpCode']))
            {
                $code = $data['otpCode'];
                print "Recieved code for $email - $code\n";
                break;
            }
            sleep(5);
        }
        return $code;
    }

    public function getPersonalAccountCredentials($login, $password, $selectedMerchantID = null, $shopID = null)
    {
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

        if (!str_starts_with($login, 'pbsrv_'))
            return ['step' => 0, 'merchants' => null, 'merchantId' => null, 'ampCookie' => null, 'mc_session_cookie' => null, 'mc_sid_cookie' => null];

        if (!waitForShopLoginLock($shopID, "shopLogin"))
        {
            return ['step' => 0, 'merchants' => null, 'merchantId' => null, 'ampCookie' => null, 'mc_session_cookie' => null, 'mc_sid_cookie' => null];
        }

        print "Login 0 - locked $shopID\n";

        $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc];
        [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/login", $headers, [], false);
        $step = 1;
        $merchants = null;

        if ($httpCode == 200)
        {
            $step = 2;
            $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $contentTypeJSON, $originIdmc, $keepAlive, $refIdmcLogin, ...$sec1];
            [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/api/p/login", $headers, ['_u' => $login, '_p' => $password, "_r_d" => false]);
            print "Login 1 response $httpCode - $response\n";

            if ($httpCode == 401)
            {
                $r = json_decode($response, true);
                if (in_array($r["errorCode"], ["MFA_SEND_FLOOD", "MFA_CODE_TOO_MANY_SEND"]) || isset($r["errorData"]["breakTimeSeconds"]))
                {
                    $breakTimeSeconds = $r["errorData"]["breakTimeSeconds"] ?? 60;
                    $breakTimeSeconds += rand(10,20);
                    sleep($breakTimeSeconds);
                    $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $contentTypeJSON, $originIdmc, $keepAlive, $refIdmcLogin, ...$sec1];
                    [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/api/p/login", $headers, ['_u' => $login, '_p' => $password, "_r_d" => false]);
                    print "Login 2 response $httpCode - $response\n";
                }
            }

            if ($httpCode == 200)
            {
                $step = 3;
                $r = json_decode($response, true);
                if (isset($r))
                {
                    if (!isset($r['redirectUrl']))
                    {
                        print "Login 3 trying to get email code\n";
                        $code = $this->getEmailOtpCode($login, 120);
                        if (!isset($code))
                        {
                            tryToGetShopLock($shopID, "shopLogin", 'unlock');
                            print "Login 4 - unlocked $shopID\n";
                            return ['step' => $step, 'merchants' => null, 'merchantId' => null, 'ampCookie' => null, 'mc_session_cookie' => null, 'mc_sid_cookie' => null];
                        }

                        $headers = ["Cookie: " . $setCookie, $ua, $acceptAll, $acceptLanguage, $acceptEnc, $keepAlive, $refIdmcLogin, ...$sec3];
                        [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/api/p/login", $headers, ['_u' => $login, '_m_c' => $code, "_r_d" => true]);
                        print "Login 5 response $httpCode - $response\n";
                        if ($httpCode != 200)
                        {
                            tryToGetShopLock($shopID, "shopLogin", 'unlock');
                            print "Login 6 - unlocked $shopID\n";
                            return ['step' => $step, 'merchants' => null, 'merchantId' => null, 'ampCookie' => null, 'mc_session_cookie' => null, 'mc_sid_cookie' => null];
                        }
                        $r = json_decode($response, true);
                    }

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
                                                            $merchantId = $selectedMerchantID ?? $r['merchants'][0]['uid'];
                                                            $merchants = $r['merchants'] ?? null;
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
        }

        $data = ['step' => $step, 'merchants' => $merchants, 'merchantId' => $merchantId ?? null, 'ampCookie' =>$ampCookie ?? null, 'mc_session_cookie' => $mc_session_cookie ?? null, 'mc_sid_cookie' => $mc_sid_cookie ?? null];
        if (isset($shopID) && $this->validatePersonalCabinetCreds($data, true))
        {
            postResults( $GLOBALS['LK_DISTR_ID'], $GLOBALS['token'], "setShopCredentials", [ 'ID' => $shopID, 'creds' => $data ] );
        }

        tryToGetShopLock($shopID, "shopLogin", 'unlock');

        return $data;
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

    public function getLKCredentialsFromArray($data): void
    {
        $this->merchantId = $data['merchantId'] ?? null;
        $this->ampCookie = $data['ampCookie'] ?? null;
        $this->mc_session_cookie = $data['mc_session_cookie'] ?? null;
        $this->mc_sid_cookie = $data['mc_sid_cookie'] ?? null;
    }

    public function getPersonalCabinetOrdersGraph($fromDate, $toDate, $LKEmail, $LKPassword, $KaspiMerchantID, $shopID, $page = 0, $perPage = 10)
    {
        $data = [
            "operationName"=>"getOrders",
            "variables" =>
                [
                    "merchantUid" => $this->merchantId,
                    "size" => $perPage,
                    "page" => $page,
                    "input" =>
                        [
                            "presetFilter" => "ARCHIVED",
                            "orderCode" => "",
                            "cityId" => "",
                            "archivedOrderStatusFilter" => ["RETURNING","COMPLETED","CANCELLED","CANCELLED_BY_MERCHANT","RETURNED"],
                            "fromDate" => date('Y-m-d', $fromDate) . 'T00:00:00+05:00',
                            "toDate" => date('Y-m-d', $toDate) . "T23:59:59+05:00"
                        ],
                    "advancedInput" => ["orderCode" => "","phoneNumber" => "","productCode" => ""],
                    "withAdvancedOrders" => false
                ],

            "query" => "query getOrders(\$merchantUid: String!, \$input: MerchantOrderInput!, \$advancedInput: MerchantOrderAdvancedInput!, \$withAdvancedOrders: Boolean!, \$page: Int!, \$size: Int, \$sort: [String!]) {\n  merchant(id: \$merchantUid) {\n    id\n    orders {\n      orders(input: \$input, page: \$page, size: \$size, sort: \$sort) @skip(if: \$withAdvancedOrders) {\n        total\n        orders {\n          ...OrdersPageFragment\n          __typename\n        }\n        __typename\n      }\n      advancedOrders(input: \$advancedInput, page: \$page, size: \$size, sort: \$sort) @include(if: \$withAdvancedOrders) {\n        total\n        orders {\n          ...OrdersPageFragment\n          __typename\n        }\n        __typename\n      }\n      __typename\n    }\n    __typename\n  }\n}\n\nfragment OrdersPageFragment on Order {\n  code\n  customer {\n    firstName\n    lastName\n    __typename\n  }\n  totalPrice\n  creationTime\n  modificationTime\n  status\n  entries {\n    isImeiRequired\n    product {\n      code\n      name\n      __typename\n    }\n    merchantProduct {\n      code\n      name\n      barcode\n      __typename\n    }\n    totalPrice\n    quantity\n    __typename\n  }\n  destination {\n    __typename\n    ... on Point {\n      id\n      name\n      enabled\n      type\n      city {\n        id\n        name\n        __typename\n      }\n      schedule {\n        weekDays {\n          openingTime\n          closingTime\n          dayOfWeek\n          __typename\n        }\n        __typename\n      }\n      pointAddress: address {\n        streetName\n        streetNumber\n        building\n        phone\n        name\n        __typename\n      }\n      __typename\n    }\n    __typename\n    ... on OrderAddress {\n      streetName\n      streetNumber\n      building\n      city {\n        id\n        name\n        __typename\n      }\n      __typename\n    }\n    __typename\n    ... on Postomat {\n      id\n      postomatAddress: address\n      city {\n        id\n        name\n        __typename\n      }\n      __typename\n    }\n  }\n  warehouse {\n    __typename\n    ... on Point {\n      id\n      name\n      enabled\n      address {\n        streetName\n        streetNumber\n        building\n        phone\n        name\n        __typename\n      }\n      city {\n        id\n        name\n        __typename\n      }\n      kaspiDelivery {\n        dailyMaxPickupTimeEnabled\n        __typename\n      }\n      __typename\n    }\n    __typename\n    ... on OrderAddress {\n      streetName\n      streetNumber\n      building\n      city {\n        id\n        name\n        __typename\n      }\n      __typename\n    }\n    __typename\n    ... on Postomat {\n      id\n      postomatAddress: address\n      city {\n        id\n        name\n        __typename\n      }\n      __typename\n    }\n  }\n  markers {\n    creationTime\n    marker\n    user {\n      name\n      __typename\n    }\n    userName\n    __typename\n  }\n  steps {\n    status\n    timeoutTime\n    step\n    plannedTime\n    additionalDays\n    __typename\n  }\n  payments {\n    __typename\n    ... on OrderLoan {\n      amount\n      signRequired\n      __typename\n    }\n    __typename\n    ... on OrderAccount {\n      amount\n      signRequired\n      __typename\n    }\n  }\n  cargoSpace\n  kaspiDelivery\n  deliveryMethod\n  deliveryZone\n  delivery {\n    actualDeliveryDate\n    isOrderArrived\n    isExpress\n    isReturnedToWarehouse\n    kdReturnedToWarehouseDate\n    kdTransmittedToCourier\n    mode\n    plannedDeliveryDate\n    returnedToWareHouseTimeoutDate\n    transmissionPlanningDate\n    assembleDate\n    __typename\n  }\n  cancelReason\n  cancelSubReason\n  moderatedReason\n  moderatedSubReason\n  moderated\n  consignments {\n    superExpressStatus\n    returnedWarehouse {\n      id\n      address\n      __typename\n    }\n    __typename\n  }\n  __typename\n}"
        ];

        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=getOrders",
            $this->getPersonalCabinetRequestHeaders("a3.0.a3"),
            $data, true);

        if ($httpCode == 401)
        {
            print "Unauthorized request to get orders, trying to refresh session\n";
            [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=getOrders",
                $this->getPersonalCabinetRequestHeaders("a3.0.a3"),
                $data, true);
        }

        $r = null;
//print "\n\n" . json_encode($data) . "\n\n";
//print "\n\n $httpCode, \n $response \n";
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r['data']['merchant']['orders']['orders']['orders'] ?? null;
    }


    public function sendPersonalCabinetOrderGraphRequest($orderCode): array
    {
        $data = ["operationName" => "getOrderDetails","variables" => ["merchantUid" => $this->merchantId, "orderCode" => $orderCode],
            "query" => "query getOrderDetails(\$merchantUid: String!, \$orderCode: String!) {\n  merchant(id: \$merchantUid) {\n    id\n    orderDetail(code: \$orderCode) {\n      code\n      cancelReason\n      cancelSubReason\n      checkoutSmsRequired\n      commentText\n      courierDetails {\n        ext\n        phone\n        __typename\n      }\n      consignments {\n        cargoFactor\n        returnedWarehouse {\n          address\n          id\n          __typename\n        }\n        trackingMapUrl\n        superExpressStatus\n        warehouse {\n          ... on Postomat {\n            id\n            city {\n              name\n              id\n              __typename\n            }\n            postomatAddress: address\n            __typename\n          }\n          ... on OrderAddress {\n            streetNumber\n            building\n            city {\n              name\n              id\n              __typename\n            }\n            streetName\n            __typename\n          }\n          ... on Point {\n            id\n            name\n            virtual\n            type\n            schedule {\n              weekDays {\n                openingTime\n                dayOfWeek\n                closingTime\n                __typename\n              }\n              __typename\n            }\n            kaspiDeliveryConfig {\n              defaultCutOffTime\n              availableCutOffTimes\n              __typename\n            }\n            kaspiDelivery {\n              pickupType\n              express\n              enabled\n              dailyMaxPickupTimeEnabled\n              schedule {\n                weekDays {\n                  dayOfWeek\n                  __typename\n                }\n                cutoff {\n                  time\n                  __typename\n                }\n                __typename\n              }\n              waybill {\n                format\n                __typename\n              }\n              __typename\n            }\n            expressDisabledUntil\n            expressDisabledAt\n            enabled\n            city {\n              name\n              id\n              __typename\n            }\n            address {\n              streetNumber\n              streetName\n              phone\n              name\n              geoPoint {\n                longitude\n                latitude\n                __typename\n              }\n              comment\n              city {\n                name\n                id\n                __typename\n              }\n              building\n              __typename\n            }\n            __typename\n          }\n          __typename\n        }\n        __typename\n      }\n      creationTime\n      customer {\n        phoneNumber\n        lastName\n        firstName\n        __typename\n      }\n      delivery {\n        transmissionPlanningDate\n        returnedToWareHouseTimeoutDate\n        plannedPickupDate\n        plannedDeliveryDate\n        mode\n        kdAssembled\n        kdTransmittedToCourier\n        kdReturnedToWarehouseDate\n        isReturnedToWarehouse\n        isExpress\n        isOrderArrived\n        assembleDate\n        actualDeliveryDate\n        plannedPointDeliveryDate\n        __typename\n      }\n      deliveryCost\n      deliveryDiscount\n      deliveryMethod\n      deliverySubsidyCost\n      deliveryZone\n      destination {\n        ... on Postomat {\n          id\n          city {\n            name\n            id\n            __typename\n          }\n          postomatAddress: address\n          __typename\n        }\n        ... on OrderAddress {\n          streetNumber\n          building\n          streetName\n          city {\n            name\n            id\n            __typename\n          }\n          __typename\n        }\n        ... on Point {\n          id\n          name\n          virtual\n          type\n          schedule {\n            weekDays {\n              openingTime\n              dayOfWeek\n              closingTime\n              __typename\n            }\n            __typename\n          }\n          kaspiDeliveryConfig {\n            defaultCutOffTime\n            availableCutOffTimes\n            __typename\n          }\n          kaspiDelivery {\n            waybill {\n              format\n              __typename\n            }\n            schedule {\n              weekDays {\n                dayOfWeek\n                __typename\n              }\n              cutoff {\n                time\n                __typename\n              }\n              __typename\n            }\n            pickupType\n            express\n            enabled\n            dailyMaxPickupTimeEnabled\n            __typename\n          }\n          expressDisabledUntil\n          expressDisabledAt\n          enabled\n          city {\n            name\n            id\n            __typename\n          }\n          pointAddress: address {\n            streetNumber\n            streetName\n            phone\n            name\n            geoPoint {\n              longitude\n              latitude\n              __typename\n            }\n            comment\n            building\n            __typename\n          }\n          __typename\n        }\n        __typename\n      }\n      entries {\n        weight\n        unit\n        totalPrice\n        quantity\n        product {\n          name\n          images {\n            baseUrl\n            paths\n            __typename\n          }\n          code\n          __typename\n        }\n        merchantProduct {\n          barcode\n          name\n          code\n          __typename\n        }\n        isImeiRequired\n        initialWeight\n        initialTotalPrice\n        initialQuantity\n        entryId\n        category {\n          name\n          code\n          __typename\n        }\n        __typename\n      }\n      kaspiDelivery\n      fiscalReceiptLink\n      markers {\n        userName\n        user {\n          name\n          __typename\n        }\n        marker\n        creationTime\n        __typename\n      }\n      merchantUid\n      modificationTime\n      payments {\n        ... on OrderLoan {\n          __typename\n          amount\n          signRequired\n        }\n        ... on OrderAccount {\n          account\n          amount\n          signRequired\n          __typename\n        }\n        __typename\n      }\n      reservedUntilDate\n      promotions {\n        ... on OrderLoanPromotion {\n          duration\n          __typename\n        }\n        ... on OrderBonusPromotion {\n          __typename\n          entries {\n            amount\n            entryNumber\n            __typename\n          }\n        }\n        __typename\n      }\n      returnRequests {\n        code\n        completionTime\n        creationTime\n        currencyCode\n        entries {\n          entryNumber\n          expectedQuantity\n          productCode\n          receivedQuantity\n          status\n          __typename\n        }\n        internalCode\n        partial\n        refundDeliveryCost\n        status\n        subtotal\n        __typename\n      }\n      state\n      status\n      steps {\n        actualTime\n        additionalDays\n        changed\n        executor {\n          name\n          __typename\n        }\n        delayed\n        plannedTime\n        status\n        step\n        timeoutTime\n        __typename\n      }\n      preOrder\n      totalPrice\n      warehouse {\n        ... on Postomat {\n          id\n          city {\n            id\n            name\n            __typename\n          }\n          postomatAddress: address\n          __typename\n        }\n        ... on OrderAddress {\n          streetNumber\n          building\n          streetName\n          city {\n            name\n            id\n            __typename\n          }\n          __typename\n        }\n        ... on Point {\n          id\n          name\n          virtual\n          type\n          schedule {\n            weekDays {\n              openingTime\n              dayOfWeek\n              closingTime\n              __typename\n            }\n            __typename\n          }\n          kaspiDeliveryConfig {\n            availableCutOffTimes\n            defaultCutOffTime\n            __typename\n          }\n          kaspiDelivery {\n            waybill {\n              format\n              __typename\n            }\n            schedule {\n              weekDays {\n                dayOfWeek\n                __typename\n              }\n              cutoff {\n                time\n                __typename\n              }\n              __typename\n            }\n            pickupType\n            express\n            enabled\n            dailyMaxPickupTimeEnabled\n            __typename\n          }\n          expressDisabledUntil\n          expressDisabledAt\n          enabled\n          city {\n            name\n            id\n            __typename\n          }\n          pointAddress: address {\n            streetNumber\n            streetName\n            phone\n            name\n            geoPoint {\n              longitude\n              latitude\n              __typename\n            }\n            comment\n            building\n            __typename\n          }\n          __typename\n        }\n        __typename\n      }\n      __typename\n    }\n    __typename\n  }\n}" ];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=getOrderDetails",
            $this->getPersonalCabinetRequestHeaders("3o.0.3o"),
            $data, true);

        return [$response, $headers, $httpCode, $setCookie, $location];
    }

    public function getPersonalCabinetOrderGraph($orderCode, $LKEmail, $LKPassword, $KaspiMerchantID, $shopID)
    {
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendPersonalCabinetOrderGraphRequest($orderCode);
        if ($httpCode == 401)
        {
            print "Order graph response 401, trying to refresh credentials\n";
            $result = $this->getPersonalAccountCredentials($LKEmail, $LKPassword, $KaspiMerchantID, $shopID);
            if ($this->validatePersonalCabinetCreds($result, true)) {
                $this->getLKCredentialsFromArray($result);

                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendPersonalCabinetOrderGraphRequest($orderCode);
                if ($httpCode == 401)
                {
                    print "Order graph response 401 after refresh, returning null\n";
                    return null;
                }
            }
            else
            {
                print "Order graph - cannot refresh credentials\n";
                return null;
            }
        }

        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
            if (!isset($r['data']['merchant']['orderDetail']))
                print "Order graph response without details: \n " . $response;
        }
        else
        {
            print "Order graph response: $httpCode - " . $response;
        }

//        print "\nOrder graph response detail: \n " . json_encode($r['data']['merchant']['orderDetail'] ?? 'null') . "\n";
        return $r['data']['merchant']['orderDetail'] ?? null;
    }

    public function getPersonalCabinetOrderPhoneGraph($orderCode)
    {
        $r = $this->getPersonalCabinetOrderGraph($orderCode);
        return $r['customer']['phoneNumber'] ?? null;
    }

    public function convertDateTimeToTimestamp($time): ?int
    {
        if (!isset($time))
            return null;
        $dt = \DateTime::createFromFormat('Y-m-d\TH:i:s.vT', $time);
        return ($dt === false) ? null : $dt->format('U');
    }

    public function getOrderDates( $orderCode, $order ): ?array
    {
        $completedDate = null;
        $returnedDate = null;

        if ($order['status'] === "RETURNED" && !empty($order['returnRequests']))
        {
            foreach($order['returnRequests'] as $r)
            {
                if (isset($r['completionTime']))
                {
                    $returnedDate = $this->convertDateTimeToTimestamp($r['completionTime']);
                }
            }
        }

        if ($order['status'] === "RETURNED" && !isset($returnedDate))
        {
            $returnedDate = $this->convertDateTimeToTimestamp($order['modificationTime']);
        }

        foreach ($order['markers'] ?? [] as $m)
        {
            if ($m['marker'] == 'COMPLETED')
            {
                $completedDate = $this->convertDateTimeToTimestamp($m['creationTime']);
                break;
            }
        }
        return (isset($completedDate) || isset($returnedDate)) ? [$orderCode, $completedDate, $returnedDate] : null;
    }

    public function getPersonalCabinetOrderDates($orderCode, $LKEmail, $LKPassword, $KaspiMerchantID, $shopID): ?array
    {
        return $this->getOrderDates( $orderCode, $this->getPersonalCabinetOrderGraph($orderCode, $LKEmail, $LKPassword, $KaspiMerchantID, $shopID) );
    }

    public function getPersonalCabinetAllOrdersFinishDate($startDate, $endDate, $LKEmail, $LKPassword, $KaspiMerchantID, $AuthshopID, $shopID = null, $LK_DISTR_ID = null, $token = null): array
    {
        $allData = [];

        $daysAdd = 2;

        $fromDate = $startDate;
        $toDate = $fromDate + $daysAdd * 86400;

        do
        {
            $dt1=date("d.m.Y H:i:s", $fromDate);
            $dt2=date("d.m.Y H:i:s", $toDate);
            $dt3=date("d.m.Y H:i:s", $endDate);

            print ("\nCurrently doing $dt1 - $dt2 of end date $dt3 \n");

            $page = 0;
            do
            {
                print("\tPage $page \r");
                $data = $this->getPersonalCabinetOrdersGraph($fromDate, $toDate, $LKEmail, $LKPassword, $KaspiMerchantID, $AuthshopID, $page);
                foreach ($data ?? [] as $order)
                {
                    $dates = null;
                    if ($order['status'] === "COMPLETED")
                        $dates = $this->getOrderDates( $order['code'], $order );
                    elseif ($order['status'] === "RETURNED")
                        $dates = $this->getPersonalCabinetOrderDates( $order['code'], $LKEmail, $LKPassword, $KaspiMerchantID, $AuthshopID );
                    if (isset($dates))
                        $allData[] = $dates;
                }
                $page++;
            }
            while (isset($data) && count($data) > 0);

            if (isset($shopID, $LK_DISTR_ID, $token))
            {
                postResults( $LK_DISTR_ID, $token, "postShopsFinishReturnDate", [ 'shopID' => $shopID, 'result' => $allData ] );
                $allData = [];
            }

            $fromDate = $toDate;
            $toDate += $daysAdd * 86400;

        } while ($toDate < $endDate + $daysAdd * 86400);

        return $allData;
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

    public function getAPIToken()
    {
        $data = ["operationName" => "getTokenApi","variables" => ["id" => $this->merchantId],"query" => "query getTokenApi(\$id: String!) {\n  merchant(id: \$id) {\n    id\n    integration {\n      token\n      __typename\n    }\n    __typename\n  }\n}" ];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=getTokenApi",
            $this->getPersonalCabinetRequestHeaders("2.0.2"),
            $data, true);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r['data']['merchant']['integration']['token'] ?? null;
    }

    public function setItemPriceWithAvailabilities($sku, $price, $PreOrderData = null)
    {
        $storecodes = [];
//        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/bff/offer-view/details?m=" . $this->merchantId . "&s=" . $sku, $this->getPersonalCabinetRequestHeaders("e.0.e"), [], false);
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/bff/offer-view/list?" .
            http_build_query(['m' => $this->merchantId, 'p' => 0, 'l' => 100, 'a' => 'true', 't' => $sku]), $this->getPersonalCabinetRequestHeaders("3f.0.3f"), [], false);
        $availabilities = [];
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
            if (isset($r))
            {
                foreach($r['data'] ?? [] as $item)
                {
                    if ($item['sku'] == $sku)
                    {
                        $date = date("Y-m-d H:i:s");
                        file_put_contents("price_changes.txt",
                            "\nDate: $date\nSKU: $sku, new price $price, preorderData: " . json_encode($PreOrderData ?? []) . "\n" .
                            "availabilities: " . json_encode($item['availabilities'] ?? []) . "\n" .
                            "stocks: " . json_encode($item['stocks'] ?? []) . "\n",
                            FILE_APPEND | LOCK_EX);

                        foreach($item['availabilities'] ?? [] as $store)
                        {
//                            $preOrderDays = !isset($PreOrderData) ? ($store['preOrder'] ?? null) : null;
                            $preOrderDays = null;
                            if (isset($PreOrderData))
                            {
                                foreach ($PreOrderData as $pData)
                                {
                                    if ($store['storeId'] == $this->merchantId . '_' . $pData['Code'])
                                    {
                                        $preOrderDays = $pData['PreOrderDays'];
                                    }
                                }
                            }

                            $avail = [ "available" => "yes", "storeId" => $store['storeId'] ];

                            if (isset($preOrderDays))
                            {
                                $avail['preOrder'] = $preOrderDays;
                            }

                            $actualAvailability = null;
                            if ((isset($store['stockSpecified']) && $store['stockSpecified']))
                            {
                                foreach($item['stocks'] ?? [] as $stock)
                                {
                                    if (isset($stock['stockLevel'][$store['storeId']]['value']))
                                        $actualAvailability = $stock['stockLevel'][$store['storeId']]['value'];
                                }
                            }

                            if ((isset($store['stockSpecified']) && $store['stockSpecified']))
                            {
                                $avail['stockCount'] = $actualAvailability ?? $store['stockCount'];
                            }

                            $availabilities[] = $avail;
                        }
                    }
                }
            }
        }

        print "sku = $sku, price => $price, Availabilities:\n";
        print_r($availabilities);
        if (empty($availabilities))
            return null;

        $data = ["merchantUid" => $this->merchantId,"sku" => $sku, "price" => $price, 'availabilities' => $availabilities];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/pricefeed/upload/merchant/process",
            $this->getPersonalCabinetRequestHeaders("f.0.f"),
            $data, true);
        $r = null;
        print($response);
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }

        $date = date("Y-m-d H:i:s");
        file_put_contents("price_changes.txt",  "\nDate: $date\nSending price change: " . json_encode($data ?? []) . "\nResponse: $response\n", 	FILE_APPEND | LOCK_EX);

        return $r;
    }

    public function getPersonalCabinetItems($LKEmail, $LKPassword, $KaspiMerchantID, $shopID, $page = 0, $perPage = 10, $onlyActive = null)
    {
        $active = "";
        if (isset($onlyActive))
        {
            $active = $onlyActive ? "&a=true" : "&a=false";
        }

        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/bff/offer-view/list?m={$this->merchantId}&p={$page}&l={$perPage}{$active}", $this->getPersonalCabinetRequestHeaders("1s.0.1s"), [], false);

        if ($httpCode == 401)
        {
            print "Personal cabinet items response 401, trying to refresh credentials\n";
            $result = $this->getPersonalAccountCredentials($LKEmail, $LKPassword, $KaspiMerchantID, $shopID);
            if ($this->validatePersonalCabinetCreds($result, true))
            {
                $this->getLKCredentialsFromArray($result);
                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/bff/offer-view/list?m={$this->merchantId}&p={$page}&l={$perPage}{$active}", $this->getPersonalCabinetRequestHeaders("1s.0.1s"), [], false);
                if ($httpCode == 401)
                {
                    print "Personal cabinet items response 401 after refresh, returning null\n";
                    return null;
                }
            }
        }

        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r;
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

    public function generateAPIToken()
    {
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=tokenGenerate",$this->getPersonalCabinetRequestHeaders("3a.0.3a"), ["operationName"=>"tokenGenerate","variables"=>["merchantId"=>$this->merchantId],"query"=>"mutation tokenGenerate(\$merchantId: String!) {\n  tokenGenerate(merchantId: \$merchantId)\n}"]);

        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r;
    }

    public function sendPriceFile($code)
    {
        $data = file_get_contents("https://my.profitbot.kz/kxml/{$code}");
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/pricefeed/upload/merchant/upload?merchantUid=" . $this->merchantId, $this->getPersonalCabinetRequestHeaders("32.0.32"),
            '', true, false, true, $data);
        print "$httpCode - " . $response . "\n";

        if (str_contains($response, "Unfinished file found"))
        {
            return "Unfinished file found";
        }

        $r = null;
        if ($httpCode == 200) {
            $r = json_decode($response, true);
        }
        return $r;
    }

    public function getFileLoadStatus($fileId, $LKEmail, $LKPassword, $KaspiMerchantID, $shopID)
    {
        [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/pricefeed/protocol/merchant/file/{$fileId}?m=" . $KaspiMerchantID, $this->getPersonalCabinetRequestHeaders("bd.0.bd"), [], false);

        if ($httpCode == 401)
        {
            print "merchant/file response 401, trying to refresh credentials, response: $response\n";
            $result = $this->getPersonalAccountCredentials($LKEmail, $LKPassword, $KaspiMerchantID, $shopID);
            if ($this->validatePersonalCabinetCreds($result, true)) {
                $this->getLKCredentialsFromArray($result);

                [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/pricefeed/protocol/merchant/file/{$fileId}?m=" . $KaspiMerchantID, $this->getPersonalCabinetRequestHeaders("bd.0.bd"), [], false);
                if ($httpCode == 401)
                {
                    print "merchant/file response 401 after refresh, returning null\n";
                    return null;
                }
            }
        }

        $r = null;
        if ($httpCode == 200) {
            $r = json_decode($response, true);
        }
        return $r['status'] ?? null;
    }
}

function crypto_rand_secure(int $min, int $max)
{
    $range = $max - $min;

    if ($range < 1) {
        return $min;
    }

    $log = ceil(log($range, 2));
    $bytes = (int) ($log / 8) + 1;
    $bits = (int) $log + 1;
    $filter = (int) (1 << $bits) - 1;

    do {
        $rnd = hexdec(bin2hex(openssl_random_pseudo_bytes($bytes, $strong)));
        $rnd = $rnd & $filter;
    } while ($rnd > $range);

    return $min + $rnd;
}

function GenerateSessionID($length)
{
    $token = "";
    $codeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
    $codeAlphabet .= "abcdefghijklmnopqrstuvwxyz";
    $codeAlphabet .= "0123456789";
    $max = strlen($codeAlphabet);

    for ($i=0; $i < $length; $i++) {
        $token .= $codeAlphabet[crypto_rand_secure(0, $max - 1)];
    }

    return $token;
}

function getLKAccessTasks($lk_distr_id, $token, $action)
{
    $data = file_get_contents("https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}");
    return json_decode($data, true);
}

function postResults($lk_distr_id, $token, $action, $arrResult)
{
//    $context = stream_context_create(['http' => [ 'header' => "Content-type: application/json\r\n", 'method' => 'POST', 'content' => json_encode( $arrResult ) ] ]);
//    file_get_contents("https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}", false, $context);

    $ch = curl_init();

    curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
    curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 10);
    curl_setopt($ch, CURLOPT_TIMEOUT, 50);
    curl_setopt($ch, CURLOPT_URL, "https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}");
    curl_setopt($ch, CURLOPT_HTTPHEADER,[ "Content-Type: application/json; charset=utf-8" ]);
    curl_setopt($ch, CURLOPT_ENCODING ,"UTF-8");
    curl_setopt($ch, CURLOPT_POST, true);
    curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode( $arrResult ));
    curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, 0);
    curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, 0);

    $response = curl_exec($ch);

//print "\n\nsent results: $action, \n" . json_encode($arrResult) . "\n with response $response \n\n";

    curl_close($ch);

}

function getAllPersonalCabinetItems($perPage, $onlyActive, $kl, $afterRegistration, $shopID, $userID, $LK_DISTR_ID, $token, $requested = false)
{
    $page = 0;
    $data = $kl->getPersonalCabinetItems($page, $perPage, $onlyActive);
    if (!isset($data['data']))
    {
        return null;
    }

    while (isset($data['data']) && count($data['data']) > 0)
    {
        postResults( $LK_DISTR_ID, $token, $requested ? "postShopItemsDataRequested" : "postShopItemsData", [ 'ID' => $shopID, 'UserID' => $userID, 'afterRegistration' => $afterRegistration, 'result' => $data['data'] ] );
        $page++;
        $data = $kl->getPersonalCabinetItems($page, $perPage, $onlyActive);
    }
}

function getAllPersonalCabinetItemsForSync($perPage, $onlyActive, $kl, $afterRegistration, $shopID, $userID, $LK_DISTR_ID, $token, $LKEmail, $LKPassword, $KaspiMerchantID, $AuthshopID, $requested = false): array
{
    $page = 0;
    $data = $kl->getPersonalCabinetItems($LKEmail, $LKPassword, $KaspiMerchantID, $AuthshopID, $page, $perPage, $onlyActive);
    if (!isset($data['data']))
    {
        return [null, null];
    }

    $total = $data['total'];
    $codes = [];

    while (isset($data['data']) && count($data['data']) > 0)
    {
        foreach ($data['data'] as $item)
            $codes[] = $item['sku'];

        postResults( $LK_DISTR_ID, $token, $requested ? "postShopItemsDataRequested" : "postShopItemsData", [ 'ID' => $shopID, 'UserID' => $userID, 'afterRegistration' => $afterRegistration, 'result' => $data['data'] ] );
        $page++;
        $data = $kl->getPersonalCabinetItems($LKEmail, $LKPassword, $KaspiMerchantID, $AuthshopID, $page, $perPage, $onlyActive);
    }

    return [$total, $codes];
}

function tryToGetShopLock($shopID, $type, $lockAction = 'lock')
{
    $action = 'manageShopLock';
    $lk_distr_id = $GLOBALS['LK_DISTR_ID'];
    $token = $GLOBALS['token'];

    $data = file_get_contents("https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}&ID={$shopID}&type={$type}&lockAction={$lockAction}");
    $r = json_decode($data, true);
    return $r['status'] ?? null;
}

function waitForShopLoginLock($shopID, $type, $timeout = 60): bool
{
    $startTime = time();
    print "Trying lock shop $shopID, type $type\n";
    while (true)
    {
        $status = tryToGetShopLock($shopID, $type);
        if ($status === 'locked')
        {
            print "Locked successfully\n";
            return true;
        }

        print "Lock status $status\n";

        if (time() - $startTime > $timeout)
        {
            print "Failed to lock after $timeout\n";
            return false;
        }

        sleep(1);
    }
}

$type = $argv[1] ?? null;

if ($type == 'requestedItemsLoader')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getRequestedItemsLoaderTasks');
        print "Checked RequestedItemsLoader at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $shopID = $task['ID'];
            $userID = $task['UserID'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted RequestedItemsLoader for ShopID {$shopID} at {$dt} - ";

            $kl = new KaspiLogin();
            $result = $task['AccessData'] ?? $kl->getPersonalAccountCredentials($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);
            $credsValid = $kl->validatePersonalCabinetCreds($result, true);
            if ($credsValid)
            {
                $kl->getLKCredentialsFromArray($result);

                $numItems = 0;
                $data = $kl->getPersonalCabinetItems($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID, 0, 10, true);
                if (!isset($data['data'], $data['total']))
                {
                    postResults( $LK_DISTR_ID, $token, "postShopItemsImportResultRequested", [ 'ID' => $shopID, 'UserID' => $userID, 'success' => false ] );
                    continue;
                }

                $numItems = $data['total'];

                $data = $kl->getPersonalCabinetItems($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID, 0, 10, false);
                if (!isset($data['data'], $data['total']))
                {
                    postResults( $LK_DISTR_ID, $token, "postShopItemsImportResultRequested", [ 'ID' => $shopID, 'UserID' => $userID, 'success' => false ] );
                    continue;
                }

                $numItems += $data['total'];
                postResults( $LK_DISTR_ID, $token, "postShopItemsDataRequestedQuant", [ 'ID' => $shopID, 'UserID' => $userID, 'totalQuant' => $numItems ] );

                [$totalActive, $codesActive] = getAllPersonalCabinetItemsForSync(25, true, $kl, false, $shopID, $userID, $LK_DISTR_ID, $token, $task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID, true);
                [$totalInactive, $codesInactive] = getAllPersonalCabinetItemsForSync(25, false, $kl, false, $shopID, $userID, $LK_DISTR_ID, $token, $task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID, true);

                if (count($codesActive) == $totalActive && count($codesInactive) == $totalInactive)
                {
                    postResults( $LK_DISTR_ID, $token, "postShopItemsForSync", [ 'ID' => $shopID, 'UserID' => $userID, 'active' => $codesActive, 'inactive' => $codesInactive ] );
                }
            }

            postResults( $LK_DISTR_ID, $token, "postShopItemsImportResultRequested", [ 'ID' => $shopID, 'UserID' => $userID, 'success' => true ] );

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}


if ($type == 'firstItemsLoader' || $type === 'periodicItemsLoader')
{
    $isFirst = $type == 'firstItemsLoader';

    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, $isFirst ? 'getFirstShopItemsImportTasks' : 'getPeriodicShopItemsImportTasks');
        print "Checked $type at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $shopID = $task['ID'];
            $userID = $task['UserID'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted $type for ShopID {$shopID} at {$dt} - ";

            $kl = new KaspiLogin();
            $result = $task['AccessData'] ?? $kl->getPersonalAccountCredentials($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);
            $credsValid = $kl->validatePersonalCabinetCreds($result, true);
            if ($credsValid)
            {
                $kl->getLKCredentialsFromArray($result);
                $stores = $kl->getPersonalCabinetStores();
                if (isset($stores))
                    postResults( $LK_DISTR_ID, $token, "postShopStoresData", [ 'ID' => $shopID, 'UserID' => $userID, 'result' => $stores ] );

                [$totalActive, $codesActive] = getAllPersonalCabinetItemsForSync(25, true, $kl, $isFirst, $shopID, $userID, $LK_DISTR_ID, $token, $task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);
                [$totalInactive, $codesInactive] = getAllPersonalCabinetItemsForSync(25, false, $kl, false, $shopID, $userID, $LK_DISTR_ID, $token, $task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);

                if (isset($codesActive, $codesInactive) && count($codesActive) == $totalActive && count($codesInactive) == $totalInactive)
                {
                    postResults( $LK_DISTR_ID, $token, "postShopItemsForSync", [ 'ID' => $shopID, 'UserID' => $userID, 'active' => $codesActive, 'inactive' => $codesInactive ] );
                }
            }

            postResults( $LK_DISTR_ID, $token, "postShopItemsImportResult", [ 'ID' => $shopID, 'UserID' => $userID, 'creds' => $result, 'credsValid' => $credsValid ] );

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}

if ($type == 'lkPriceChanger')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getPriceDumpingTasks');
        print "Checked getPriceDumpingTasks at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $shopID = $task['ShopID'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted getPriceDumpingTasks for ShopID {$shopID}, ID {$task['ID']} at {$dt} - ";

            $kl = new KaspiLogin();
            $creds = $task['AccessData'] ?? $kl->getPersonalAccountCredentials($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);
            if ($kl->validatePersonalCabinetCreds($creds, true))
            {
                $kl->getLKCredentialsFromArray($creds);
                $result = $kl->setItemPriceWithAvailabilities($task['Code'], $task['Price'], $task['PreOrderData'] ?? null);

                if (!isset($result))
                {
                    $creds = $kl->getPersonalAccountCredentials($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);
                    if ($kl->validatePersonalCabinetCreds($creds, true))
                    {
                        $kl->getLKCredentialsFromArray($creds);
                        $result = $kl->setItemPriceWithAvailabilities($task['Code'], $task['Price'], $task['PreOrderData'] ?? null);
                    }
                }

                postResults( $LK_DISTR_ID, $token, "postPriceDumpingTasksResult", [ 'ID' => $task['ID'], 'result' => $result ] );
            }

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}

if ($type == 'lkOrdersFinishDate')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getShopsFinishDate');
        print "Checked lkOrdersFinishDate at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['shops'] ?? [] as $shop)
        {
            $shopID = $shop['ShopID'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted lkOrdersFinishDate for ShopID {$shopID} at {$dt} - ";

            $kl = new KaspiLogin();
            $result = $shop['AccessData'] ?? $kl->getPersonalAccountCredentials($shop['LKEmail'], $shop['LKPassword'], $shop['KaspiMerchantID'], $shopID);
            if ($kl->validatePersonalCabinetCreds($result, true))
            {
                $kl->getLKCredentialsFromArray($result);

                if (isset($shop['OrdersList']))
                {
                    $ordersData = [];
                    $orders = explode(",", $shop['OrdersList']);
                    foreach($orders as $code)
                    {
                        if (!empty($code))
                        {
                            $dates = $kl->getPersonalCabinetOrderDates($code, $shop['LKEmail'], $shop['LKPassword'], $shop['KaspiMerchantID'], $shopID);
                            if (isset($dates))
                                $ordersData[] = $dates;
                        }
                    }
                }
                else
                {
                    $ordersData = $kl->getPersonalCabinetAllOrdersFinishDate($shop['MinDate'], $shop['MaxDate'], $shop['LKEmail'], $shop['LKPassword'], $shop['KaspiMerchantID'], $shopID, $shop['ShopID'], $LK_DISTR_ID, $token);
                }

                if (!empty($ordersData))
                {
                    postResults( $LK_DISTR_ID, $token, "postShopsFinishReturnDate", [ 'shopID' => $shop['ShopID'], 'result' => $ordersData ] );
                }
            }

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(10);
    }
}

if (!isset($type) || $type == 'lkAccessChecker')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getTasks');
        print "Checked at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted at {$dt}\n";
            $kl = new KaspiLogin();
            $taskData = json_decode($task['TaskData'], true);
            if (!isset($taskData['Email']) || !isset($taskData['Password']))
            {
                $result = ['step' => 0];
            }
            else
            {
                $merchantId = $taskData['MerchantID'] ?? null;

                $result = $kl->getPersonalAccountCredentials($taskData['Email'], $taskData['Password']);
                if (isset($merchantId) && isset($result['merchants']) && in_array($merchantId, array_column($result['merchants'], 'uid')))
                {
                    $result['merchantId'] = $merchantId;
                }

                $credsValid = $kl->validatePersonalCabinetCreds($result, true);
                if ($credsValid)
                {
                    $kl->getLKCredentialsFromArray($result);
                    $result['token'] = $kl->getAPIToken();
                }
            }

            postResults( $LK_DISTR_ID, $token, "postResult", [ 'taskID' => $task['ID'], 'result' => $result ] );

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}

if ($type == 'lkOrdersClientPhoneBulk')
{
    $clientsData = explode("\r\n", file_get_contents('client_phones.txt'));

    $arr = [];
    foreach ($clientsData as $client)
    {
        [$ID, $ShopID, $Code, $CustomerID, $Phone, $KaspiID, $LKEmail, $LKPassword] = explode("\t", $client);
        $arr[] = [ 'ID' => $ID, 'ShopID' => $ShopID, 'Code' => $Code, 'CustomerID' => $CustomerID, 'Phone' => $Phone, 'KaspiID' => $KaspiID, 'LKEmail' => $LKEmail, 'LKPassword' => $LKPassword, "Processed" => false ];
    }

    for ($i = 0; $i<count($arr); $i++)
    {
        $item = $arr[$i];

        if ($item['Processed'])
            continue;

        $kl = new KaspiLogin();
        $result = $kl->getPersonalAccountCredentials($item['LKEmail'], $item['LKPassword'], $item['KaspiMerchantID']);
        if ($kl->validatePersonalCabinetCreds($result, true))
        {
            $kl->getLKCredentialsFromArray($result);
            $phone = $kl->getPersonalCabinetOrderPhoneGraph($item['Code']);
            if (isset($phone))
            {
                print "{$item['CustomerID']}\t{$phone}\n";
                $arr[$i]['Processed'] = true;
                $arr[$i]['Phone'] = $phone;
                for ($j = $i + 1; $j < count($arr); $j++)
                {
                    $item2 = $arr[$j];
                    if (!$item2['Processed'] && $item2['CustomerID'] == $item['CustomerID'])
                    {
                        $arr[$j]['Phone'] = $phone;
                        $arr[$j]['Processed'] = true;
                    }
                }
            }

            for ($j = $i + 1; $j < count($arr); $j++)
            {
                $item2 = $arr[$j];
                if (!$item2['Processed'] && $item2['ShopID'] == $item['ShopID'])
                {
                    $phone = $kl->getPersonalCabinetOrderPhoneGraph($item2['Code']);
                    if (isset($phone))
                    {
                        print "{$item2['CustomerID']}\t{$phone}\n";
                        $arr[$j]['Processed'] = true;
                        $arr[$j]['Phone'] = $phone;
                        for ($k = 0; $k < count($arr); $k++)
                        {
                            $item3 = $arr[$k];
                            if (!$item3['Processed'] && $item3['CustomerID'] == $item2['CustomerID'])
                            {
                                $arr[$k]['Phone'] = $phone;
                                $arr[$k]['Processed'] = true;
                            }
                        }
                    }
                }
            }
        }
        else
        {
            for ($j = $i + 1; $j < count($arr); $j++)
            {
                if (!$arr[$j]['Processed'] && $arr[$j]['ShopID'] == $item['ShopID'])
                    $arr[$j]['Processed'] = true;
            }
        }
    }
}

if ($type == 'lkOrdersClientPhone')
{
    $accessDatas = [];

    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getClientsPhonesToParse');
        print "Checked getClientsPhonesToParse at " . date("d.m.Y H:i:s", time()) . "\r";

        $arr = [];
        foreach ($data['tasks'] ?? [] as $task)
        {
            $task["Processed"] = false;
            $arr[] = $task;
        }

        if (count($data['tasks'])>0)
            print "\nGot data to parse count " . count($data['tasks'] ?? []) . " at " . date("d.m.Y H:i:s", time()) . "\n";


        $phonesData = [];

        for ($i = 0; $i<count($arr); $i++)
        {
            $item = $arr[$i];

            $shopID = $item['ShopID'];

            if ($item['Processed'])
                continue;

            $kl = new KaspiLogin();
            $result = $accessDatas[$shopID] ?? $kl->getPersonalAccountCredentials($item['LKEmail'], $item['LKPassword'], $item['KaspiMerchantID']);

            if ($kl->validatePersonalCabinetCreds($result, true))
            {
                $accessDatas[$shopID] = $result;

                $kl->getLKCredentialsFromArray($result);
                $phone = $kl->getPersonalCabinetOrderPhoneGraph($item['Code']);
                if (isset($phone))
                {
                    $phonesData[] = [ 'CustomerID' => $item['CustomerID'], 'Phone' => $phone ];
                    $arr[$i]['Processed'] = true;
                    for ($j = $i + 1; $j < count($arr); $j++)
                    {
                        $item2 = $arr[$j];
                        if (!$arr[$j]['Processed'] && $arr[$j]['CustomerID'] == $item['CustomerID'])
                        {
                            $arr[$j]['Processed'] = true;
                        }
                    }
                }
                else
                {
                    unset($accessDatas[$shopID]);
                }

                for ($j = $i + 1; $j < count($arr); $j++)
                {
                    $item2 = $arr[$j];
                    if (!$item2['Processed'] && $item2['ShopID'] == $item['ShopID'])
                    {
                        $phone = $kl->getPersonalCabinetOrderPhoneGraph($item2['Code']);
                        if (isset($phone))
                        {
                            $phonesData[] = [ 'CustomerID' => $item2['CustomerID'], 'Phone' => $phone ];
                            $arr[$j]['Processed'] = true;
                            for ($k = 0; $k < count($arr); $k++)
                            {
                                $item3 = $arr[$k];
                                if (!$item3['Processed'] && $item3['CustomerID'] == $item2['CustomerID'])
                                {
                                    $arr[$k]['Processed'] = true;
                                }
                            }
                        }
                        else
                        {
                            unset($accessDatas[$shopID]);
                        }
                    }
                }
            }
            else
            {
                for ($j = $i + 1; $j < count($arr); $j++)
                {
                    if (!$arr[$j]['Processed'] && $arr[$j]['ShopID'] == $item['ShopID'])
                        $arr[$j]['Processed'] = true;
                }
            }
        }

        if (!empty($phonesData))
        {
            postResults($LK_DISTR_ID, $token, "postClientsPhonesParsed", $phonesData);
            print "\n\nSent results getClientsPhonesToParse count " . count($phonesData) . "\n\n" . print_r($phonesData, true) . " at " . date("d.m.Y H:i:s", time()) . "\n\n\n";
        }
    }
}

if ($type == 'lkGetSendPriceFileTasks')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'lkGetSendPriceFileTasks');
        print "Checked lkGetSendPriceFileTasks at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $shopID = $task['ID'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted lkGetSendPriceFileTasks for ShopID {$shopID}, LoadID {$task['LoadID']} at {$dt} - ";

            $kl = new KaspiLogin();
            $creds = $task['AccessData'] ?? $kl->getPersonalAccountCredentials($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID );
            if ($kl->validatePersonalCabinetCreds($creds, true))
            {
                $kl->getLKCredentialsFromArray($creds);
                $result = $kl->sendPriceFile($task['KaspiDownloadCode']);

                if (!isset($result))
                {
                    $creds = $kl->getPersonalAccountCredentials($task['LKEmail'], $task['LKPassword'], $task['KaspiMerchantID'], $shopID);
                    if ($kl->validatePersonalCabinetCreds($creds, true))
                    {
                        $kl->getLKCredentialsFromArray($creds);
                        $result = $kl->sendPriceFile($task['KaspiDownloadCode']);
                    }
                }

                postResults($LK_DISTR_ID, $token, "lkPostSendPriceFileResult", [ 'ID' => $task['ID'], 'LoadID' => $task['LoadID'], 'result' => $result ] );
            }

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}

if ($type == 'getShopsToGetCredentials')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getShopsToGetCredentials');
        print "Checked getShopsToGetCredentials at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $shopID = $task['ID'];
            $email = $task['LKEmail'];
            $password = $task['LKPassword'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted getShopsToGetCredentials for ShopID {$shopID} at {$dt} - ";

            $kl = new KaspiLogin();
            $result = $kl->getPersonalAccountCredentials($email, $password, $task['KaspiMerchantID']);
            $credsValid = $kl->validatePersonalCabinetCreds($result, true);
            if ($kl->validatePersonalCabinetCreds($result, true))
            {
                postResults( $LK_DISTR_ID, $token, "setShopCredentials", [ 'ID' => $shopID, 'creds' => $result ] );
            }

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}

if ($type == 'getFileLoadStatus')
{
    while (true)
    {
        $data = getLKAccessTasks($LK_DISTR_ID, $token, 'getFileLoadStatus');
        print "Checked getFileLoadStatus at " . date("d.m.Y H:i:s", time()) . "\r";
        foreach ($data['tasks'] ?? [] as $task)
        {
            $shopID = $task['ShopID'];
            $email = $task['LKEmail'];
            $password = $task['LKPassword'];

            $start = microtime(true);
            $dt=date("d.m.Y H:i:s", time());
            print "\nStarted getFileLoadStatus for ShopID {$shopID} at {$dt} - ";

            $kl = new KaspiLogin();
            $creds = $task['AccessData'] ?? $kl->getPersonalAccountCredentials($email, $password, $task['KaspiMerchantID']);
            if ($kl->validatePersonalCabinetCreds($creds, true))
            {
                $kl->getLKCredentialsFromArray($creds);
                $status = $kl->getFileLoadStatus($task['FileID'], $email, $password, $task['KaspiMerchantID'], $shopID);
                if (isset($status) && $status == "FLUSHED")
                {
                    print "File load status is FLUSHED, FileID {$task['FileID']}\n";
                    postResults( $LK_DISTR_ID, $token, "postFileLoadStatus", [ 'ID' => $task['ID'] ] );
                }
            }

            print "Elapsed: " . (microtime(true) - $start) . "\n";
        }

        sleep(1);
    }
}
