<?php

namespace ProfitBot\Modules\Services\Methods;

use ProfitBot\Modules\ExternalApi\KaspiAPI;
use ProfitBot\Modules\ExternalApi\KaspiImport;
use ProfitBot\Modules\Http\Controllers\ProfileController;
use ProfitBot\Modules\Http\Request;
use ProfitBot\Modules\Services\ServiceAbstraction;

class AddKaspiShop extends ServiceAbstraction
{
    public function perform(): void
    {
        $name = $this->request->getParameter('name');
        $apikey = trim($this->request->getParameter('apikey'));
        $id = $this->request->getParameter('id');
        $lkemail = $this->request->getParameter('lkemail');
        $lkpassword = $this->request->getParameter('lkpassword');
        $taxpercent = $this->request->getParameter('taxpercent');

        $marketinglogin = $this->request->getParameter('marketinglogin');
        $marketingpassword = $this->request->getParameter('marketingpassword');
        $dosendprice = $this->request->getParameter('dosendprice') ?? 0;
        $selectedMerchantID = $this->request->getParameter('selectedMerchantID');

        if (isset($marketinglogin))
        {
            $marketinglogin = trim(onlydigits($marketinglogin));
            if (empty($marketinglogin))
            {
                $marketinglogin = null;
                $marketingpassword = null;
            }
        }

        if (isset($marketingpassword))
        {
            $marketingpassword = str_replace('&#039;', "'", $marketingpassword);;
        }

        $lkpassword = str_replace("&quot;", '"', $lkpassword);

        $this->result['resultCode'] = 400;

        if ( empty($name) || !isset($id) || empty($lkemail) || empty($lkpassword) || !is_numeric($taxpercent) || $taxpercent < 0 || $taxpercent > 100 ) {
            $this->result['errorMessage'] = 'Обязательные поля не заполнены';
            $this->result['errorCode'] = 'required_fields_empty';
            return;
        }

        if (!isset($this->request->userID)) {
            $this->result['errorMessage'] = 'Нужно войти в аккаунт';
            $this->result['errorCode'] = 'need_to_login';
            return;
        }

        $this->result['needSelectMerchant'] = false;


        $addShopMsgID = 4;
        if ($id == 0)
        {
            $data = $this->request->db->select_row_array("Select ID from UserKaspiShops where UserID=? and Token=? and DeletedDate IS NULL", [ $this->request->userID, $apikey ]);
            if (isset($data) && count($data) > 0)
            {
                $this->result['errorMessage'] = 'Такой магазин уже добавлен';
                $this->result['errorCode'] = 'shop_already_added';
                return;
            }

            $tc = new \ProfitBot\Modules\ExternalApi\KaspiImport( $this->request, $apikey, 0, $this->request->userID );
            $creds = $tc->getPersonalAccountCredentials($lkemail, $lkpassword, $selectedMerchantID);
            $credsValid = $tc->validatePersonalCabinetCreds($creds, true);

            if ($credsValid && count($creds['merchants']) > 1 && (!isset($selectedMerchantID) || !in_array($selectedMerchantID, array_column($creds['merchants'], 'uid'))))
            {
                $this->result['needSelectMerchant'] = true;
                $merchants = '';
                foreach(array_column($creds['merchants'], 'uid') as $merchantId)
                {
                    $merchants .= '<option value="'.$merchantId.'">'.$merchantId.'</option>';
                }
                $this->result['merchants'] = $merchants;
                $this->result['success'] = false;
                $this->result['resultCode'] = 200;
                return;
            }

            if ($credsValid && empty($apikey))
            {
                $apikey = $creds['token'] ?? '';
            }

            $kaspi = new KaspiAPI($this->request, $apikey, 0, $this->request->userID);
            $tokenValid = $kaspi->validateToken();

            $marketingCredsValid = true;
            $marketingCreds = null;
            if (isset($marketinglogin) && isset($marketingpassword) && $credsValid)
            {
                $marketingCreds = $tc->marketingLogin($marketinglogin, $marketingpassword, $creds['merchantId']);
                $marketingCredsValid = $tc->validateMarketingCreds($marketingCreds);
            }

            if (!$credsValid || !$tokenValid || !$marketingCredsValid)
            {
                $this->result['errorMessage'] = 'Неверный API ключ или данные личного кабинета';
                $this->result['errorCode'] = 'invalid_api_key_or_lk_creds';
                $this->result['credsValid'] = $credsValid;
                $this->result['tokenValid'] = $tokenValid;
                $this->result['marketingCredsValid'] = $marketingCredsValid;
                return;
            }

            $downloadCode = GenerateSessionID(15);
            $this->request->db->do("insert into UserKaspiShops (UserID, Name, Token, IsTokenValid, FirstImportFinished, LKEmail, LKPassword, KaspiDownloadCode, KaspiMerchantID, TaxPercent, MarketingLogin, MarketingPassword, DoSendKaspiPrice) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", [$this->request->userID, $name, $apikey, 1, 0, $lkemail, $lkpassword, $downloadCode, $creds['merchantId'], $taxpercent, $marketinglogin, $marketingpassword, $dosendprice]);
            $id = $this->request->db->GetLastID();

            if (isset($marketinglogin) && isset($marketingpassword) && $marketingCredsValid)
            {
                $this->request->db->do("update UserKaspiShops set MarketingLastUpdated=GetDate(), UseMarketingAccess=1, MarketingSession=?, MarketingMerchantID=? where ID = ?", [ $marketingCreds['cookie'], $marketingCreds['merchantid'], $id ]);
            }

            $userData = $this->request->db->select_row_array('SELECT Phone FROM Users where ID = ?', [ $this->request->userID ]);
            $userPhone = $userData[0]['Phone'] ?? null;

            $this->request->sendWhatsappMessage($userPhone,"Құттықтаймыз! 🎉 Сіздің дүкеніңіз қосылды.
Қазір біз деректерді жүктеп жатырмыз — тауарлар, остатки, тапсырыс тарихы ⏳
Әдетте бұл 15 минутқа дейін уақыт алады.

Барлығы дайын болғанда — сізге жазып, неден бастау керегін түсіндіреміз 👌

Егер сұрақтарыңыз болса — бізге жазыңыз 😊

Поздравляем! 🎉 Ваш магазин добавлен.
Сейчас загружаем данные — товары, остатки, историю заказов ⏳
Обычно это занимает до 15 минут.

Как только всё будет готово — напишем и подскажем, с чего начать 👌

Если будут вопросы — пишите нам 😊");
        }
        else
        {
            $shopData = $this->request->db->select_row_array("Select * from UserKaspiShops where UserID=? and ID=? and DeletedDate IS NULL", [ $this->request->userID, $id ]);
            if (!isset($shopData) || count($shopData) === 0)
            {
                $this->result['errorMessage'] = 'Магазина не существует';
                $this->result['errorCode'] = 'shop_doesnot_exists';
                return;
            }

            $data = $this->request->db->select_row_array("Select ID from UserKaspiShops where UserID=? and Token=? and ID<>? and DeletedDate IS NULL", [ $this->request->userID, $apikey, $id ]);
            if (isset($data) && count($data) > 0)
            {
                $this->result['errorMessage'] = 'Такой магазин уже добавлен';
                $this->result['errorCode'] = 'shop_already_added';
                return;
            }

            $tc = new \ProfitBot\Modules\ExternalApi\KaspiImport( $this->request, $apikey, (int)$id, $this->request->userID );
            $creds = $tc->getPersonalAccountCredentials($lkemail, $lkpassword, $shopData[0]['KaspiMerchantID']);
            $credsValid = $tc->validatePersonalCabinetCreds($creds, true);
            if ($credsValid && empty($apikey))
            {
                $apikey = $creds['token'] ?? '';
            }

            $kaspi = new KaspiAPI($this->request, $apikey, (int)$id, $this->request->userID);
            $tokenValid = $kaspi->validateToken();

            $marketingCredsValid = true;
            if (isset($marketinglogin) && isset($marketingpassword) && $credsValid)
            {
                $marketingCreds = $tc->marketingLogin($marketinglogin, $marketingpassword, $creds['merchantId']);
                $marketingCredsValid = $tc->validateMarketingCreds($marketingCreds);
            }

            if (!$credsValid || !$tokenValid || !$marketingCredsValid)
            {
                $this->result['errorMessage'] = 'Неверный API ключ или данные личного кабинета';
                $this->result['errorCode'] = 'invalid_api_key_or_lk_creds';
                $this->result['credsValid'] = $credsValid;
                $this->result['tokenValid'] = $tokenValid;
                $this->result['marketingCredsValid'] = $marketingCredsValid;
                return;
            }

            $this->request->db->do("update UserKaspiShops set IsTokenValid = ? where ID = ?", [ $tokenValid ? 1 :0, $id ]);
            $this->request->insertUserMessage($tokenValid ? 5 : 6, [ 'UserID' => $this->request->userID, 'ShopID' => $id, 'SessionID' => $this->request->getSession() ]);

            $this->request->db->do("update UserKaspiShops set Name = ?, Token = ?, LKEmail=?, LKPassword=?,KaspiMerchantID=?, TaxPercent=?, MarketingLogin=?, MarketingPassword=?, DoSendKaspiPrice=? where ID = ?", [$name, $apikey, $lkemail, $lkpassword, $creds['merchantId'], $taxpercent, $marketinglogin, $marketingpassword, $dosendprice, $id]);

            $addShopMsgID = 23;

            if ($shopData[0]['LKEmail'] != $lkemail || $shopData[0]['LKPassword'] != $lkpassword)
            {
                $this->request->db->do("update UserKaspiShops set LKAccessDataRenewDate=NULL, UseLKAccess=NULL where ID = ?", [ $id ]);
            }

            if ($shopData[0]['MarketingLogin'] != $marketinglogin || $shopData[0]['MarketingPassword'] != $marketingpassword)
            {
                $this->request->db->do("update UserKaspiShops set MarketingLastUpdated=NULL, UseMarketingAccess=NULL where ID = ?", [ $id ]);
            }

            if (isset($marketinglogin) && isset($marketingpassword) && $marketingCredsValid)
            {
                $this->request->db->do("update UserKaspiShops set MarketingLastUpdated=GetDate(), UseMarketingAccess=1, MarketingSession=?, MarketingMerchantID=? where ID = ?", [ $marketingCreds['cookie'], $marketingCreds['merchantid'], $id ]);
            }
        }

        $this->request->insertUserMessage($addShopMsgID, [ 'UserID' => $this->request->userID, 'ShopID' => $id, 'SessionID' => $this->request->getSession() ]);

        $this->result['success'] = true;
        $this->result['resultCode'] = 200;
        $this->result['data'] = ProfileController::getKaspiShopsTable($this->request);
    }
}