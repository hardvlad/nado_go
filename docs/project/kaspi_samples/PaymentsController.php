<?php

namespace ProfitBot\Modules\Http\Controllers;

use ProfitBot\Modules\EPay;
use ProfitBot\Modules\ExternalApi\GreenAPI;
use ProfitBot\Modules\Http\Request;
use ProfitBot\Modules\Libs\QRCode;

class PaymentsController extends CommonController
{
    public function __construct(Request $request)
    {
        parent::__construct($request);
    }

    public function ExtendService(Request $request): void
    {
        $key = $request->getParameter('key');
        $serviceId = $request->getParameter('serviceid');
        if (!isset($key))
        {
            $this->sendPage('500.html', 500);
            return;
        }

        $serviceData = $this->request->db->select_row_array("select a.ID,a.UserID,a.ServiceID,b.Price from UserServices a inner join Services b on b.ID=a.ServiceID where OperationKey=?", [$key]);
        if (!isset($serviceData) || count($serviceData) === 0)
        {
            $this->sendPage('500.html', 500);
            return;
        }

        if (isset($serviceId) && $serviceData[0]['ServiceID'] != $serviceId)
        {
            $newServiceData = $this->request->db->select_row_array("select * from Services where ID=?", [$serviceId]);
            if (isset($newServiceData) && count($newServiceData) > 0)
            {
                $this->createPaymentRequestAndRedirect( $serviceData[0]['UserID'] , $newServiceData[0]['ID'], $newServiceData[0]['Price'], $serviceData[0]['ID'] );
                return;
            }
        }

        $this->createPaymentRequestAndRedirect( $serviceData[0]['UserID'] , $serviceData[0]['ServiceID'], $serviceData[0]['Price'], $serviceData[0]['ID'] );
    }

    protected function createPaymentRequestAndRedirect($userID, $serviceID, $price, $userServiceID=null): void
    {
        $orderId = '';
        $res = $this->request->db->do(" insert into PaymentRequests (OrderID,UserID,ServiceID,Status,Price,UserServiceID) values (?,?,?,?,?,?)",[ $orderId, $userID, $serviceID, 'new', $price, $userServiceID ]);
        if ($res === false)
        {
            $this->sendPage('500.html',500);
            return;
        }
        $requestId = $this->request->db->GetLastID();

        $this->request->AddToOutStrings("orderid", $requestId);
        $this->request->AddToOutStrings("price", $price);
        $this->request->AddToOutStrings("pricekopek", $price*100);

        $balance = new ProfileController($this->request)->getReferralBalance();
        if ($balance > 0)
        {
            $this->request->AddToOutStrings("balance", $balance + 0);
            $finalPrice = $price - $balance;
            if ($finalPrice < 0)
                $finalPrice = 0;
            $this->request->AddToOutStrings("finalPrice", $finalPrice + 0);

            $this->sendPage('payment-with-bonuses.html',200);
            return;
        }

        $this->sendPage('payment-select-type.html',200);
    }

    public function BuyNewService(Request $request): void
    {
        if (!$this->checkAccess($request))
        {
            return;
        }

        $serviceId = $request->getParameter('serviceid');
        if (!isset($serviceId))
        {
            $this->sendPage('500.html',500);
            return;
        }

        $serviceData = $this->request->db->select_row_array("select * from Services where ID=?",[$serviceId]);
        if (!isset($serviceData) || count($serviceData) === 0)
        {
            $this->sendPage('500.html',500);
            return;
        }

        $this->createPaymentRequestAndRedirect( $this->request->userID, $serviceId, $serviceData[0]['Price'] );
    }

    public function PaymentSuccess(Request $request): void
    {
        $orderId = $request->getParameter('orderid');
        if (!isset($orderId))
        {
            $this->sendPage('500.html',500);
            return;
        }

        $paymentRequestData = $this->request->db->select_row_array("select * from PaymentRequests where ID=?",[ $orderId ]);
        if (!isset($paymentRequestData) || count($paymentRequestData) === 0)
        {
            $this->sendPage('500.html',500);
            return;
        }

        if (!isset($paymentRequestData[0]['Status']) || $paymentRequestData[0]['Status'] !== 'success')
        {
            $this->sendPage('payment-failed.html',500);
            return;
        }

        $res = $this->request->db->do("update PaymentRequests set Status='success', PaidDate=GetDate() where ID=?",[$orderId]);
        if ($res === false)
        {
            $this->sendPage('payment-failed.html',500);
            return;
        }

//        $this->createOrProlongService($paymentRequestData[0]['UserID'], $paymentRequestData[0]['ServiceID'], $paymentRequestData[0]['UserServiceID'], $paymentRequestData[0]['ID']);

        $this->sendPage('payment-success.html',200);
    }

    public function PaymentFailure(Request $request): void
    {
        if (!$this->checkAccess($request))
        {
            return;
        }

        $orderId = $request->getParameter('orderid');
        if (!isset($orderId))
        {
            $this->sendPage('payment-failed.html',500);
            return;
        }

        $paymentRequestData = $this->request->db->select_row_array("select * from PaymentRequests where ID=? and UserID=?",[ $orderId, $this->request->userID ]);
        if (!isset($paymentRequestData) || count($paymentRequestData) === 0)
        {
            $this->sendPage('payment-failed.html',500);
            return;
        }

        $this->sendPage('payment-failed.html',200);
    }

    public function createOrProlongService($userId, $serviceId, $userServiceId, $paymentRequestId): void
    {
        $serviceData = $this->request->db->select_row_array("select * from Services where ID=?",[$serviceId]);
        if (!isset($serviceData) || count($serviceData) === 0)
        {
            return;
        }

        if (isset($userServiceId))
        {
            $userServiceData = $this->request->db->select_row_array("select * from UserServices where ID=? and UserID=? and IsActive=1 and DeletedDate IS NULL",[$userServiceId, $userId]);
            if (!isset($userServiceData) || count($userServiceData) === 0)
                $userServiceId = null;
        }

        if (!isset($userServiceId))
        {
            if (str_starts_with($serviceData[0]['Code'], 'whatsapp'))
            {
                $ga = new GreenAPI($this->request, $userId);
                $instanceId = $ga->partnerCreateInstance($userId, $serviceData[0]['PeriodDays'], $serviceId, $userServiceId);
                if (!isset($instanceId))
                {
                    $this->request->SendMessageToTelegram("GreenAPI instance was not created");
                    return;
                }

                $this->request->SendMessageToTelegramToUser($userId, "Ваш экземпляр для номера WhatApp успешно создан. Через минуту вы сможете привязать номер WhatApp.", null, $instanceId);

                $this->request->db->do("update PaymentRequests set UserServiceID=(select UserServiceID from UserWhatsAppInstances where ID=?),PaidTillDate=(select PaidTillDate from UserWhatsAppInstances where ID=?) where ID=?",[$instanceId, $instanceId, $paymentRequestId]);
/*
                $userGa = new GreenAPI($this->request, $userId, $instanceId);
                do
                {
                    sleep(1);
                    $state = $userGa->getStateInstance();
                    if (isset($state) && $state === 'notAuthorized')
                    {
                        break;
                    }
                } while (true);
*/
            }
            if (str_starts_with($serviceData[0]['Code'], 'profitbot'))
            {
                $OperationKey = GenerateSessionID(20);
                $this->request->db->do("insert into UserServices (UserID, ServiceID, PaidTillDate, IsActive, OperationKey) values (?, ?, CAST(DateAdd(SECOND,-1,DATEADD(day, {$serviceData[0]['PeriodDays']}, GetDate())) AS DATE), 1, ?)", [ $userId, $serviceId, $OperationKey ]);
                $userServiceID = $this->request->db->GetLastID();

                $this->request->db->do("update PaymentRequests set UserServiceID=?,PaidTillDate=(select PaidTillDate from UserServices where ID=?) where ID=?",[$userServiceID, $userServiceID, $paymentRequestId]);
                $this->request->SendMessageToTelegramToUser($userId,"Сервис ProfitBot успешно подключен. Услуга оплачена до " . date('d.m.Y', strtotime("+{$serviceData[0]['PeriodDays']} days")) , $paymentRequestId);
            }
        }
        else
        {
            $this->request->db->do("update UserServices set PaidTillDate=DateAdd(SECOND,-1,DateAdd(day,{$serviceData[0]['PeriodDays']},PaidTillDate)) where ID=?",[ $userServiceId ]);
            $this->request->db->do("update PaymentRequests set PaidTillDate=(select PaidTillDate from UserServices where ID=?) where ID=?",[ $userServiceId, $paymentRequestId ]);
            if (str_starts_with($serviceData[0]['Code'], 'whatsapp'))
            {
                $this->request->db->do("update UserWhatsAppInstances set PaidTillDate=DateAdd(SECOND,-1,DateAdd(day,{$serviceData[0]['PeriodDays']},PaidTillDate)) where UserServiceID=?",[ $userServiceId ]);
                $data = $this->request->db->select_row_array("select ID,PaidTillDate from UserWhatsAppInstances where UserServiceID=?",[$userServiceId]);
                if (isset($data) && count($data) > 0)
                {
                    $this->request->SendMessageToTelegramToUser($userId,"Сервис WhatsApp успешно продлен до " . sqlDateTimeToDotDate( $data[0]['PaidTillDate'] ) );
                }
            }
        }
    }

    public function EpaySuccess(Request $request)
    {
        $orderId = $request->getParameter('orderid');
        $data = file_get_contents("php://input");
        $this->request->db->do("Insert into PaymentResponse (OrderID,URL,Status,Response) values (?, ?, ?, ?)",[$orderId, $_SERVER['REQUEST_URI'], "success", $data]);

        $this->request->SendMessageToTelegram("EPAY Success Payment {$orderId} : {$data}");

        $decodedData = json_decode($data, true);

        $paymentRequestData = $this->request->db->select_row_array("select pr.*,s.Name,u.RegisteredByRefUserID,t.[Percent] from PaymentRequests pr inner join Services s on pr.ServiceID=s.ID inner join Users u on u.ID=pr.UserID inner join ReferralTerms t on t.ID=u.ReferralTermID where pr.ID=?",[ $orderId ]);
        if (!isset($paymentRequestData) || count($paymentRequestData) === 0 || $paymentRequestData[0]['OrderID'] !== $decodedData['secret_hash'])
        {
            $this->sendPage('500.html',500);
            return;
        }

        $this->request->SendMessageToTelegramToUser($paymentRequestData[0]['UserID'],"Успешная оплата суммы {$paymentRequestData[0]['Price']} тенге за услугу '{$paymentRequestData[0]['Name']}'", $paymentRequestData[0]['ID']);

        if (isset($paymentRequestData[0]['Status']) && $paymentRequestData[0]['Status'] === 'success')
        {
            $this->sendPage('500.html',500);
            return;
        }

        $res = $this->request->db->do("update PaymentRequests set Status='success', PaidDate=GetDate() where ID=?",[$paymentRequestData[0]['ID']]);
        if ($res === false)
        {
            $this->sendPage('500.html',500);
            return;
        }

        $this->applyPostPaymentActions($paymentRequestData);

        $this->createOrProlongService($paymentRequestData[0]['UserID'], $paymentRequestData[0]['ServiceID'], $paymentRequestData[0]['UserServiceID'], $paymentRequestData[0]['ID']);
    }

    public function EpayFailure(Request $request)
    {
        $orderId = $request->getParameter('orderid');
        $this->request->db->do("Insert into PaymentResponse (OrderID,URL,Status,Response) values (?, ?, ?, ?)",[$orderId, $_SERVER['REQUEST_URI'], "failure", file_get_contents("php://input")]);
    }

    private function addReferralPaymentBonus($RegisteredByRefUserID, $paymentRequestID, $Price, $Percent): void
    {
        $bonus = round($Price * $Percent / 100);
        if ($bonus == 0)
        {
            return;
        }

        $this->request->db->do("insert into ReferralPayments (UserID,PaymentRequestID,Amount) values (?,?,?)",[ $RegisteredByRefUserID, $paymentRequestID, $bonus ]);

        $this->request->SendMessageToTelegramToUser($RegisteredByRefUserID,"Начислен реферальный бонус в размере {$bonus} тенге");
    }

    public function ContinuePaymentWithBonus(Request $request)
    {
        $requestId = $request->getParameter('orderid');
        $payWithBonus = $request->getParameter('payWithBonus');

        if (!isset($requestId, $payWithBonus))
        {
            $this->sendPage('500.html',500);
            return;
        }

        $paymentRequestData = $this->request->db->select_row_array("select * from PaymentRequests where ID=?",[ $requestId ]);
        if (!isset($paymentRequestData) || count($paymentRequestData) === 0)
        {
            $this->sendPage('500.html',500);
            return;
        }

        $balance = new ProfileController($this->request)->getReferralBalance();
        $balance += 0;
        if ( $balance <= 0 )
            $payWithBonus = 0;

        $finalPrice = $paymentRequestData[0]['Price'];
        $price = $finalPrice;
        $useBonus = 0;

        if ($payWithBonus == 1)
        {
            $useBonus = ($balance >= $price) ? $price : $balance;
            $finalPrice = $price - $useBonus;

            if ($finalPrice == 0)
            {
                $res = $this->request->db->do("update PaymentRequests set Status='success', Price=?, UseBonusSum=?, PaidDate=GetDate() where ID=?",[$finalPrice, $useBonus, $paymentRequestData[0]['ID']]);
                if ($res === false)
                {
                    $this->sendPage('500.html',500);
                    return;
                }

                $res = $this->request->db->do("insert into ReferralPayments (UserID, PaymentRequestID, Amount, IsPayOut) values (?, ?, ?, 1) ",[$paymentRequestData[0]['UserID'], $requestId, $useBonus]);
                if ($res === false)
                {
                    $this->sendPage('500.html',500);
                    return;
                }

                $this->createOrProlongService($paymentRequestData[0]['UserID'], $paymentRequestData[0]['ServiceID'], $paymentRequestData[0]['UserServiceID'], $paymentRequestData[0]['ID']);
                $this->sendPage('payment-success.html',200);
                return;
            }
        }

        $res = $this->request->db->do("update PaymentRequests set Price=?, UseBonusSum=? where ID=?",[$finalPrice, $useBonus, $paymentRequestData[0]['ID']]);
        if ($res === false)
        {
            $this->sendPage('500.html',500);
            return;
        }

        $this->request->AddToOutStrings("orderid", $requestId);
        $this->request->AddToOutStrings("price", $finalPrice);
        $this->request->AddToOutStrings("pricekopek", $finalPrice*100);

        $this->sendPage('payment-select-type.html',200);
    }

    public function getKaspiQR(Request $request)
    {
        $amount = $request->getParameter('sum') ?? '10';
        $code = $request->getParameter('code') ?? '10235';

        $content = "https://kaspi.kz/pay/ProfitBotKZ?service_id=12296&17974=$code&amount=$amount";

        $logo = imagecreatefromstring((new QRCode($content, ['w' => 300, 'h' => 300]))->get_png_data());

        $im = imagecreatefrompng(__DIR__ . '/../../../../httpdocs/images/kaspi.png');

        imagecopy($im, $logo, 14, 110, 0, 0, 300, 300);

        $request->SetContentTypePNG();
        $request->PrintHTMLHeader();
        imagepng($im);
    }

    public function SelectPaymentMethod(Request $request): void
    {
        if (!$this->checkAccess($request))
        {
            return;
        }

        $orderid = $request->getParameter('orderid');
        if (!isset($orderid))
        {
            $this->sendPage('500.html',500);
            return;
        }

        $serviceData = $this->request->db->select_row_array("select ID, Price, UseBonusSum from PaymentRequests where ID=?",[ $orderid ]);
        if (!isset($serviceData) || count($serviceData) === 0)
        {
            $this->sendPage('500.html',500);
            return;
        }

        $this->request->db->do("update PaymentRequests set PaymentTypeID=2 where ID=?", [ $orderid ]);

        $this->RedirectToEpay($serviceData[0]['ID'], $serviceData[0]['Price'] - ($serviceData[0]['UseBonusSum'] ?? 0));
    }

    public function KaspiIntegration(Request $request): void
    {
        $command = $request->getParameter('command');
        $txn_id = $request->getParameter('txn_id');
        $txn_date = $request->getParameter('txn_date');
        $account = $request->getParameter('account');
        $sum = $request->getParameter('sum');

        $code = str_replace('payments/kaspi/', '', $request->path);

        $ip = $request->ClientIP;

        $request->InsertToLog("Kaspi Request: IP: $ip, command: $command, txn_id: $txn_id, account: $account, sum: $sum");

        if (!isset($txn_id, $account, $sum, $command) || $_ENV['KASPI_SECURE_URL'] != $code || !($ip == '194.187.247.152')
        )
        {
            $this->response->setData(['result' => 5, 'path' => $request->path, 'code' => $code ])->send();
            return;
        }

        if ($command === 'check')
        {
            $data = $this->request->db->select_row_array("select ID, Price from PaymentRequests where ID=?",[ $account ]);

            if (isset($data[0]['ID']))
            {
                $result = ['result' => 0, 'txn_id' => $txn_id, 'sum' => round($data[0]['Price'], 2)];
            }
            else
            {
                $result = ['result' => 1];
            }

        }
        elseif ($command === 'pay')
        {
            $data = $this->request->db->select_row_array("select ID, Response from KaspiTransactions where Txn_ID=? and Command=?",[ $txn_id, $command ]);
            if (isset($data[0]['ID']))
            {
                $this->response->setData(json_decode($data[0]['Response'], true))->send();
                return;
            }

            $data = $this->request->db->select_row_array("select pr.*,s.Name,u.RegisteredByRefUserID,t.[Percent] from PaymentRequests pr inner join Services s on pr.ServiceID=s.ID inner join Users u on u.ID=pr.UserID inner join ReferralTerms t on t.ID=u.ReferralTermID where pr.ID=?",[ $account ]);
            if (isset($data[0]['ID']))
            {
                if ($data[0]['Price'] != $sum)
                {
                    $result = ['result' => 5];
                }
                else
                {
                    $result = ["result" => 0, "txn_id" => $txn_id, "prv_txn_id" => $account, "sum" => round($sum, 2)];
                    $request->db->do("update PaymentRequests set Status='success', PaidDate=GetDate(), PaymentTypeID=1 where ID=?",[$account]);
                    $this->createOrProlongService($data[0]['UserID'], $data[0]['ServiceID'], $data[0]['UserServiceID'], $data[0]['ID']);

                    $this->applyPostPaymentActions($data);

                }
            }
            else
            {
                $result = ["result" => 1];
            }
        }

        $this->request->db->do('insert into KaspiTransactions (IP, Command, Txn_ID, Txn_Date, Account, TxnSum, Response) values (?, ?, ?, ?, ?, ?, ?)', [ $ip, $command, $txn_id, $txn_date, $account, $sum, json_encode($result ?? []) ]);

        $this->response->setData($result ?? ["result" => 5])->send();
    }

    protected function RedirectToEpay($requestId, $price): void
    {
        $epay = new EPay($this->request);
        $domain = "https://" . $_SERVER['HTTP_HOST'];
        $postLink = $domain . "/payments/post/epay/success?orderid=" . $requestId;
        $failurePostLink = $domain . "/payments/post/epay/failure?orderid=" . $requestId;
        $token = $epay->getToken($requestId, $price, $postLink, $failurePostLink);
        if (!isset($token)) {
            $this->sendPage('500.html', 500);
            return;
        }
        $orderId = $epay->getSecretHash();
        $this->request->db->do("update PaymentRequests set OrderID=? where ID=?", [$orderId, $requestId]);

        $epay->redirectToPayment($token, $requestId, $price, '', '', $domain . "/payments/success?orderid=" . $requestId, $postLink, $failurePostLink);
    }

    public function KaspiCheckPaymentStatus(Request $request): void
    {
        $orderId = $request->getParameter('orderid');
        if (!isset($orderId))
        {
            $this->response->setData(['success' => false, 'errorMessage' => 'Order ID is required'])->setCode(400)->send();
            return;
        }

        $paymentRequestData = $this->request->db->select_row_array("select Status from PaymentRequests where ID=?", [ $orderId ]);
        if (!isset($paymentRequestData) || count($paymentRequestData) === 0)
        {
            $this->response->setData(['success' => false, 'errorMessage' => 'Payment request not found'])->setCode(400)->send();
            return;
        }

        $this->response->setData(['success' => true, 'errorMessage' => '', 'payment_status' => $paymentRequestData[0]['Status']])->send();
    }

    protected function applyPostPaymentActions(array $data): void
    {
        if (isset($data[0]['RegisteredByRefUserID'])) {
            $this->addReferralPaymentBonus($data[0]['RegisteredByRefUserID'], $data[0]['ID'], $data[0]['Price'], $data[0]['Percent']);
        }

        if (isset($data[0]['UseBonusSum']) && $data[0]['UseBonusSum'] > 0) {
            $this->request->db->do("insert into ReferralPayments (UserID, PaymentRequestID, Amount, IsPayOut) values (?, ?, ?, 1) ", [$data[0]['UserID'], $data[0]['ID'], $data[0]['UseBonusSum']]);
        }
    }
}