<?php

declare(strict_types=1);

namespace ProfitBot\Modules;

use ProfitBot\Modules\Http\Request;
use ProfitBot\Modules\Http\Response;

class EPay
{
    protected Request $request;
    protected string $url;
    protected string $libUrl;
    protected string $ClientID;
    protected string $ClientSecret;
    protected string $TerminalID;
    protected string $token;

    protected string $secret_hash;
    public function __construct(Request $request)
    {
        $this->request = $request;
        $suffix = $_ENV['EPAY_USE_TEST'] === 'true' ? "_TEST" : "_PROD";
        $this->url = $_ENV['EPAY_URL' . $suffix];
        $this->libUrl = $_ENV['EPAY_LIB' . $suffix];
        $this->ClientID = $_ENV['EPAY_ClientID' . $suffix];
        $this->ClientSecret = $_ENV['EPAY_ClientSecret' . $suffix];
        $this->TerminalID = $_ENV['EPAY_TerminalID' . $suffix];
    }

    public function getSecretHash(): string
    {
        return $this->secret_hash;
    }

    public function getToken($invoiceID, $amount, $postLink, $failurePostLink): array|null
    {
        $this->secret_hash = GenerateSessionID(20);
        $fields = [
            'grant_type'      => 'client_credentials',
            'scope'           => 'payment usermanagement',
            'client_id'       => $this->ClientID,
            'client_secret'   => $this->ClientSecret,
            'invoiceID'       => $invoiceID,
            'amount'          => $amount,
            'currency'        => "KZT",
            'terminal'        => $this->TerminalID,
            'postLink'        => $postLink,
            'failurePostLink' => $failurePostLink,
            'secret_hash'     => $this->secret_hash
        ];

        $fields_string = http_build_query($fields);

        $ch = curl_init();

        curl_setopt($ch, CURLOPT_URL, $this->url);
        curl_setopt($ch, CURLOPT_POST, true);
        curl_setopt($ch, CURLOPT_POSTFIELDS, $fields_string);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, FALSE);

        $result = curl_exec($ch);

        $json_result = json_decode($result, true);
        if (!curl_errno($ch) &&  curl_getinfo($ch, CURLINFO_HTTP_CODE) === 200 && isset($json_result["access_token"]))
            return $json_result;

        return null;
    }

    public function redirectToPayment($token, $invoiceID, $amount, $email, $phone, $backLink, $postLink, $failurePostLink): void
    {
        $hbp_payment_object = (object) [
            "invoiceId" => $invoiceID,
            "backLink" => $backLink,
            "failureBackLink" => $backLink,
            "postLink" => $postLink,
            "failurePostLink" => $failurePostLink,
            "language" => "RU",
            "description" => "Заказ $invoiceID",
            "accountId" => "",
            "terminal" => $this->TerminalID,
            "amount" => $amount,
            "currency" => "KZT",
            "auth" => (object)$token,
            "phone" => $phone,
            "email" => $email
        ];

        if (isset($token))
        {
            (new Response(200))->addHeader('Content-Type', 'text/html')->setData("<script src='{$this->libUrl}'></script><script>halyk.pay(" .  json_encode($hbp_payment_object) . ")</script>") ->send();
        }
    }
}