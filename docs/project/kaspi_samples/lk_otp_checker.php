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

    protected $redirectURL;

    public function sendKaspiRequest($url, $sendHeaders, $data, $doPost = true)
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
        curl_setopt($ch, CURLOPT_HTTPHEADER, $sendHeaders);
        if ($doPost) {
            curl_setopt($ch, CURLOPT_POST, true);
            curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode($data));
        }
        curl_setopt($ch, CURLOPT_ENCODING, "UTF-8");
        curl_setopt($ch, CURLOPT_HEADERFUNCTION, function ($curl, $header) use (&$headers, &$setCookie, &$location) {
            $len = strlen($header);
            $headers[] = $header;
            $header = explode(':', $header, 2);
            if (count($header) === 2 && strtolower($header[0]) === 'set-cookie') {
                $setCookie = strtok(trim($header[1]), ';');
            }

            if (count($header) === 2 && strtolower($header[0]) === 'location') {
                $location = strtok(trim($header[1]), ';');
            }

            return $len;
        });

        $response = curl_exec($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);

        curl_close($ch);
        return [$response, $headers, $httpCode, $setCookie, $location];
    }

    public function sendOTP($phone)
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

        $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc];
        [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://kaspi.kz/mc/", $headers, [], false);
        $step = 1;

        if ($httpCode == 200)
        {
            $step = 2;
            $secondPart = GenerateSessionID(9);
            $ampCookie = 'amp_6e9c16=' . GenerateSessionID(12) . '-_' . GenerateSessionID(8) . '...' . $secondPart . '.' . $secondPart;
            $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $keepAlive, $refKaspiMc, "Cookie: {$ampCookie}.0.0.0", ...$sec5];
            [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://kaspi.kz/yml/ms/feat/p/ft/pre/e?d=true", $headers, [], false);

            if ($httpCode == 200)
            {
                $step = 3;
                $this->ampCookie = $ampCookie;
                $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $originKaspi, $keepAlive, $refKaspi,"Cookie: {$ampCookie}.0.0.0", ...$sec6];
                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/s/m", $headers, [], false);

                if ($httpCode == 401)
                {
                    $step = 4;
                    $mc_session_cookie = $setCookie;
                    $this->mc_session_cookie = $mc_session_cookie;

                    $headers = [$ua, $acceptXml, $acceptLanguage, $acceptEnc, $keepAlive, $refKaspi,"Cookie: {$ampCookie}.0.0.0; " . $setCookie, ...$sec2, "TE: trailers"];
                    [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/oauth2/authorization/1", $headers, [], false);

                    if ($httpCode == 302)
                    {
                        $step = 5;
                        $mc_arr_cookie = $setCookie;
                        $this->redirectURL = $location;
                        $headers = [$ua, $acceptXml, $acceptLanguage, $acceptEnc, $keepAlive, "Cookie: {$ampCookie}.0.0.0", ...$sec2, "TE: trailers"];
                        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($location, $headers, [], false);

                        if ($httpCode == 302)
                        {
                            $step = 6;
                            $MS_AUTH_SSO_Cookie = $setCookie;

                            $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, "Cookie: {$ampCookie}.0.0.0; " . $MS_AUTH_SSO_Cookie];
                            [$response, $headers, $httpCode, $setCookie] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/login", $headers, [], false);

                            if ($httpCode == 200)
                            {
                                $step = 7;
                                $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $contentTypeJSON, $keepAlive, $originIdmc, $refIdmcLogin, "Cookie: {$ampCookie}.0.0.0; " . $MS_AUTH_SSO_Cookie, ...$sec1];
                                $data = ["_ph" => $phone];
                                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/api/p/login", $headers, $data);

                                if ($httpCode == 200)
                                {
                                    $step = 8;

                                    return [
                                        'step' => $step,
                                        'redirectUrl' => $this->redirectURL . '&continue',
                                        'ampCookie' => $this->ampCookie,
                                        'mc_session_cookie' => $this->mc_session_cookie,
                                        'mc_arr_cookie' => $mc_arr_cookie,
                                        'MS_AUTH_SSO_Cookie' => $MS_AUTH_SSO_Cookie,
                                        'phone' => $phone,
                                    ];
                                }
                            }
                        }
                    }
                }
            }
        }

        return [ 'step' => $step ];
    }

    public function getAPIToken($merchantId, $headers)
    {
        $data = ["operationName" => "getTokenApi","variables" => ["id" => $merchantId],"query" => "query getTokenApi(\$id: String!) {\n  merchant(id: \$id) {\n    id\n    integration {\n      token\n      __typename\n    }\n    __typename\n  }\n}" ];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=getTokenApi",
            $headers,
            $data, true);
        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r['data']['merchant']['integration']['token'] ?? null;
    }

    public function generateAPIToken($merchantId, $headers)
    {
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/mc/facade/graphql?opName=tokenGenerate",
            $headers, ["operationName"=>"tokenGenerate","variables"=>["merchantId"=>$merchantId],"query"=>"mutation tokenGenerate(\$merchantId: String!) {\n  tokenGenerate(merchantId: \$merchantId)\n}"]);

        $r = null;
        if ($httpCode == 200)
        {
            $r = json_decode($response, true);
        }
        return $r["data"]["tokenGenerate"] ?? null;
    }

    public function verifyOTP($data, $otp, $name, $email)
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

        $step = 9;
        $ampCookie = $data['ampCookie'];
        $mc_session_cookie = $data['mc_session_cookie'];
        $mc_arr_cookie = $data['mc_arr_cookie'];
        $MS_AUTH_SSO_Cookie = $data['MS_AUTH_SSO_Cookie'];
        $redirectUrl = $data['redirectUrl'];

        $apiToken = null;

        $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, $contentTypeJSON, $keepAlive, $originIdmc, $refIdmcLogin, "Cookie: {$ampCookie}.0.0.0; " . $MS_AUTH_SSO_Cookie, ...$sec1];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://idmc.shop.kaspi.kz/api/p/login", $headers, ["_c" => $otp]);

        $merchants = null;

        if ($httpCode == 200)
        {
            $step = 10;
            $MS_AUTH_SSO_Cookie = $setCookie;

            $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, "Cookie: {$ampCookie}.0.0.0; " . $MS_AUTH_SSO_Cookie, $refIdmcLogin];
            [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($redirectUrl, $headers, [], false);

            if ($httpCode == 302)
            {
                $step = 11;
                $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, "Cookie: {$ampCookie}.0.0.0; " . $mc_session_cookie . "; " . $mc_arr_cookie, $refIdmc];
                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($location, $headers, [], false);
                if ($httpCode == 302)
                {
                    $step = 12;
                    $mc_sid_cookie = $setCookie;
                    $headers = [$ua, $acceptAll, $acceptLanguage, $acceptEnc, "Cookie: {$ampCookie}.0.0.0"];
                    [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest($location, $headers, [], false);
                    if ($httpCode == 200)
                    {
                        $step = 13;
                        $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.2.0.2; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/s/m", $headers, [], false);

                        if ($httpCode == 200)
                        {
                            $step = 14;
                            $r = json_decode($response, true);

                            $merchants = $r['merchants'];

                            if ( count($merchants) == 1 )
                            {
                                $merchantId = $r['merchants'][0]['uid'];

                                $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.88.0.88; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                                [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/user-assignments/api/v1/mc/users/registered?m=" . $merchantId, $headers, [], false);
                                if ($httpCode == 200)
                                {
                                    $step = 15;
                                    $accountsBefore = $response;

                                    $headersToken = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.2.0.2; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                                    $apiToken = $this->getAPIToken($merchantId, $headersToken);
                                    if (!isset($apiToken))
                                    {
                                        $headersGenerateToken = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.3a.0.3a; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                                        $apiToken = $this->generateAPIToken($merchantId, $headersGenerateToken);
                                    }

                                    $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.88.0.88; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                                    [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/user-assignments/api/v1/mc/users/add-email", $headers, [
                                        "name" => $name,
                                        "email" => $email,
                                        "cityId" => null,
                                        "roles" => ["ACCEPT_ORDER_PICKUP","COMPLETE_ORDER_PICKUP","ACCEPT_ORDER_DELIVERY","COMPLETE_ORDER_DELIVERY","ACCEPT_KASPI_DELIVERY_ORDER","RETURN_ORDER","KASPI_DELIVERY_RETURN","MANAGE_OFFERS","MANAGE_QUALITY_CONTROL","DOWNLOAD_ACTIVE_ARCHIVE_ORDERS","KASPI_MARKETING","MANAGE_SETTINGS"],
                                        "pointName" => null,
                                        "contactPhone" => null,
                                        "merchantUid" => $merchantId,
                                    ]);
                                    if ($httpCode == 200)
                                    {
                                        $step = 16;
                                    }
                                    else
                                    {
                                        print "Email creation error:\n $response\n $httpCode\n";
                                    }
                                }
                            }
                            else
                            {
                                return [ 'step' => $step, 'needSelectMerchant' => true, 'merchants' => $merchants, 'ampCookie' => $ampCookie, 'mc_session_cookie' => $mc_session_cookie, 'mc_sid_cookie' => $mc_sid_cookie ];
                            }

                        }
                    }
                }
            }
        }
        return [ 'step' => $step, 'token' => $apiToken, 'httpCode' => $httpCode, 'needSelectMerchant' => false, 'merchantID' => $merchantId ?? null ];
    }

    public function verifyOTPwithMerchantID($data, $otp, $name, $email)
    {
        $ua = "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0";
        $acceptLanguage = "Accept-Language: en-US,en;q=0.5";
        $acceptEnc = "Accept-Encoding: gzip, deflate, br, zstd";
        $contentTypeJSON = "Content-Type: application/json";
        $keepAlive = "Connection: keep-alive";
        $refKaspi = "Referer: https://kaspi.kz/";
        $sec4 = ["x-auth-version: 3", "DNT: 1", "Sec-GPC: 1", "Sec-Fetch-Dest: empty", "Sec-Fetch-Mode: cors", "Sec-Fetch-Site: same-site", "Priority: u=4", "TE: trailers"];
        $originKaspi = "Origin: https://kaspi.kz";

        $merchantId = $data['selectedMerchantId'];
        $ampCookie = $data['ampCookie'];
        $mc_session_cookie = $data['mc_session_cookie'];
        $mc_sid_cookie = $data['mc_sid_cookie'];

        $step=14;

        $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.88.0.88; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
        [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/user-assignments/api/v1/mc/users/registered?m=" . $merchantId, $headers, [], false);
        if ($httpCode == 200)
        {
            $step = 15;
            $accountsBefore = $response;

            $headersToken = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.2.0.2; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
            $apiToken = $this->getAPIToken($merchantId, $headersToken);
            if (!isset($apiToken)) {
                $headersGenerateToken = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.3a.0.3a; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
                $apiToken = $this->generateAPIToken($merchantId, $headersGenerateToken);
            }

            $headers = [$ua, "Accept: */*", $acceptLanguage, $acceptEnc, $refKaspi, $contentTypeJSON, $originKaspi, $keepAlive, "Cookie: {$ampCookie}.88.0.88; {$mc_session_cookie}; {$mc_sid_cookie}", ...$sec4];
            [$response, $headers, $httpCode, $setCookie, $location] = $this->sendKaspiRequest("https://mc.shop.kaspi.kz/user-assignments/api/v1/mc/users/add-email", $headers, [
                "name" => $name,
                "email" => $email,
                "cityId" => null,
                "roles" => ["ACCEPT_ORDER_PICKUP", "COMPLETE_ORDER_PICKUP", "ACCEPT_ORDER_DELIVERY", "COMPLETE_ORDER_DELIVERY", "ACCEPT_KASPI_DELIVERY_ORDER", "RETURN_ORDER", "KASPI_DELIVERY_RETURN", "MANAGE_OFFERS", "MANAGE_QUALITY_CONTROL", "DOWNLOAD_ACTIVE_ARCHIVE_ORDERS", "KASPI_MARKETING", "MANAGE_SETTINGS"],
                "pointName" => null,
                "contactPhone" => null,
                "merchantUid" => $merchantId,
            ]);

            if ($httpCode == 200)
            {
                $step = 16;
            }
            else
            {
                print "Email creation error:\n $response\n $httpCode\n";
            }
        }

        return [ 'step' => $step, 'token' => $apiToken ?? null, 'httpCode' => $httpCode, 'needSelectMerchant' => false, 'merchantID' => $merchantId ?? null ];

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

function formatPhone(string $string)
{
    return "+7 (".substr($string, 1, 3).") " . substr($string, 4, 3)."-".substr($string, 7, 2)."-".substr($string, 9, 2);
}

function getLKAccessTasks($lk_distr_id, $token, $action)
{
    $data = file_get_contents("https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}");
    return json_decode($data, true);
}

function postResults($lk_distr_id, $token, $action, $arrResult)
{
    $context = stream_context_create(['http' => [ 'header' => "Content-type: application/json\r\n", 'method' => 'POST', 'content' => json_encode( $arrResult ) ] ]);
    file_get_contents("https://my.profitbot.kz/api/v1/lkAccessTasks?action={$action}&id={$lk_distr_id}&token={$token}", false, $context);
}

$type = $argv[1] ?? null;

if (!isset($type) || $type == 'lkOTPSender')
{
    while (true)
    {
        $tasks = getLKAccessTasks($LK_DISTR_ID, $token, 'getLKOTPSenderTasks');
        print "Checked lkOTPSender at " . date("d.m.Y H:i:s", time()) . "\r";
        if (isset($tasks['tasks']) && count($tasks['tasks']) > 0)
        {
            foreach ($tasks['tasks'] as $task)
            {
                $start = microtime(true);
                $dt=date("d.m.Y H:i:s", time());
                print "\nStarted lkOTPSender at {$dt}\n";

                $kl = new KaspiLogin();
                $data = $kl->sendOTP(formatPhone($task['Phone']));
                $OTPSendResult = (isset($data['step']) && $data['step'] == 8) ? 1 : 0;
                postResults($LK_DISTR_ID, $token, 'postOTPSenderResult', [ 'ID' => $task['ID'], 'RequestData' => $data, 'OTPSendResult' => $OTPSendResult ]);

                print "Elapsed: " . (microtime(true) - $start) . "\n";
            }
        }
        sleep(1);
    }
}

if ($type == 'lkOTPChecker')
{
    while (true)
    {
        $tasks = getLKAccessTasks($LK_DISTR_ID, $token, 'getLKOTPCheckerTasks');
        print "Checked lkOTPChecker at " . date("d.m.Y H:i:s", time()) . "\r";
        if (isset($tasks['tasks']) && count($tasks['tasks']) > 0)
        {
            foreach ($tasks['tasks'] as $task)
            {
                $start = microtime(true);
                $dt=date("d.m.Y H:i:s", time());
                print "\nStarted lkOTPChecker at {$dt}\n";

                $kl = new KaspiLogin();

                $requestData = json_decode($task['RequestData'], true);

                if (isset($requestData['selectedMerchantId']))
                {
                    $data = $kl->verifyOTPwithMerchantID($requestData, $task['OTPCode'], $task['Name'], $task['Email']);
                }
                else
                {
                    $data = $kl->verifyOTP($requestData, $task['OTPCode'], $task['Name'], $task['Email']);
                }

                print "Result: ";
                print_r($data);
                $UserCreateResult = (isset($data['step']) && $data['step'] == 16) ? 1 : 0;
                postResults($LK_DISTR_ID, $token, 'postOTPCheckerResult', [ 'ID' => $task['ID'], 'UserCreateResult' => $UserCreateResult, 'token' => $data['token'] ?? null, 'result' => $data ]);

                print "Elapsed: " . (microtime(true) - $start) . "\n";
            }
        }
        sleep(1);
    }
}

