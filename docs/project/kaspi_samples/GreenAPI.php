<?php

namespace ProfitBot\Modules\ExternalApi;
use ProfitBot\Modules\Http\Request;

class GreenAPI
{
    private string|null $apiDomain;
    private string|null $apiInstance;
    private string|null $apiKey;

    private string $partnerDomain;
    private string $partnerApiKey;

    private $userId;
    private $instanceId;

    private $instanceData;

    protected Request $request;

    public function __construct($request, $userid = null, $instanceId = null)
    {
        $this->request = $request;

        $this->partnerDomain = $_ENV['GREEN_API_PARTNER_DOMAIN'];
        $this->partnerApiKey = $_ENV['GREEN_API_PARTNER_KEY'];

        if (!isset($userid, $instanceId))
        {
            $this->userId = null;
            $this->instanceId = null;

            $this->apiDomain = $_ENV['GREEN_API_DOMAIN'];
            $this->apiInstance = $_ENV['GREEN_API_INSTANCE'];
            $this->apiKey = $_ENV['GREEN_API_KEY'];
        }
        else
        {
            $this->setInstanceUserID($userid, $instanceId);
        }
    }

    public function setInstanceUserID($userid, $instanceId): void
    {
        $this->userId = null;
        $this->instanceId = null;

        $this->apiDomain = null;
        $this->apiInstance = null;
        $this->apiKey = null;

        $this->instanceData = null;

        $rows = $this->request->db->select_row_array('SELECT * FROM UserWhatsappInstances WHERE ID = ? AND UserID = ?', [$instanceId, $userid]);
        if (isset($rows[0]))
        {
            $this->userId = $userid;
            $this->instanceId = $instanceId;

            $this->apiDomain = $rows[0]['Domain'];
            $this->apiInstance = $rows[0]['InstanceID'];
            $this->apiKey = $rows[0]['Token'];

            $this->instanceData = $rows[0];
        }
    }

    protected function constructUrl($method): string|null
    {
        if (!isset($this->apiDomain, $this->apiInstance, $this->apiKey))
            return null;

        return "https://{$this->apiDomain}/waInstance{$this->apiInstance}/{$method}/{$this->apiKey}";
    }

    protected function constructPartnerUrl($method): string|null
    {
        if (!isset($this->partnerDomain, $this->partnerApiKey))
            return null;

        return "https://{$this->partnerDomain}/partner/{$method}/{$this->partnerApiKey}";
    }

    public function sendData($method, $data, $isPartner, $isGet = false): array|null
    {
        $url = $isPartner ? $this->constructPartnerUrl($method) : $this->constructUrl($method);

        if (!isset($this->apiDomain, $this->apiInstance, $this->apiKey))
            return null;

        $startTime = getCurrentMilliseconds();

        $headers = ['Content-Type: application/json'];

        $encodedData = json_encode($data);
        $curl = curl_init($url);
        curl_setopt($curl, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($curl, CURLOPT_HTTPHEADER, $headers);
        if (!$isGet)
        {
            curl_setopt($curl, CURLOPT_POST, true);
            curl_setopt($curl, CURLOPT_POSTFIELDS, $encodedData);
        }

        $response = curl_exec($curl);
        $code = curl_getinfo($curl, CURLINFO_HTTP_CODE);

        curl_close($curl);

        $duration = getDiffCurrentTime($startTime);

        $this->request->db->do("insert into APICallsGreenApi (UserID, WhatsappInstanceID, Method, ResponseCode, Duration, Parameters, Response) values (?, ?, ?, ?, ?, ?, ?)", [$this->userId, $this->instanceId, $method, $code, $duration, $encodedData, $response]);

        return [$response, $code, $duration];
    }

    public function sendMessage(int $userId, int $orderId, string $phone, string $message)
    {
        $data = $this->sendData('sendMessage', [ 'chatId' => onlydigits($phone) . '@c.us', 'message' => $message ], false);
        if (isset($data[0]))
        {
            $data = json_decode($data[0], true);
            return $data['idMessage'] ?? null;
        }
        return null;
    }

    public function getAvatar(string $phone)
    {
        [$response, $code, $duration] = $this->sendData('getAvatar', [ 'chatId' => onlydigits($phone) . '@c.us' ], false);
        if ($code == 429)
            return -1;

        if (isset($response))
        {
            $data = json_decode($response, true);
            return isset($data['available']) && $data['available'] ? $data['urlAvatar'] : null;
        }
        return null;
    }

    public function getStateInstance()
    {
        $data = $this->sendData('getStateInstance', [], false, true);
        if (!isset($data) || $data[1] !== 200)
            return null;

        $data = json_decode($data[0], true);
        if (isset($data['stateInstance']))
        {
            if ($data['stateInstance'] == 'authorized' && isset($this->instanceData) && !isset($this->instanceData['Phone']))
            {
                $data = $this->getWaSettings();
                if (isset($data['phone']))
                {
                    $this->request->db->do("update UserWhatsAppInstances set Phone=? where ID=?", [$data['phone'], $this->instanceId]);
                    $this->request->SendMessageToTelegramToUser($this->userId, "Номер WhatApp {$data['phone']} успешно привязан. Вы можете настроить нужные интеграции.", null, $this->instanceId);

                }
            }
            if ($data['stateInstance'] == 'notAuthorized' && isset($this->instanceData) && isset($this->instanceData['Phone']))
            {
                $this->request->db->do("update UserWhatsAppInstances set Phone=null where ID=?", [$this->instanceId]);
                $this->request->SendMessageToTelegramToUser($this->userId, "Номер WhatApp {$this->instanceData['Phone']} отвязан. Настроенные интеграции больше не работают.", null, $this->instanceId);
            }
            return $data['stateInstance'];
        }

        return null;
    }

    public function logoutInstance()
    {
        $data = $this->sendData('logout', [], false, true);
        if (!isset($data) || $data[1] !== 200)
            return null;

        $data = json_decode($data[0], true);
        if (isset($data['isLogout']))
            return $data['isLogout'];

        return null;
    }

    public function getInstanceQR()
    {
        $data = $this->sendData('qr', [], false, true);
        if (!isset($data) || $data[1] !== 200)
            return null;

        $data = json_decode($data[0], true);
        if (isset($data['type']) && $data['type'] === 'qrCode' && isset($data['message']))
            return $data['message'];

        return null;
    }

    public function getWaSettings()
    {
        $data = $this->sendData('getWaSettings', [], false, true);
        if (!isset($data) || $data[1] !== 200)
            return null;

        $data = json_decode($data[0], true);
        if (isset($data))
            return $data;

        return null;
    }

    public function PerformWebHook(): void
    {
        $payload = file_get_contents('php://input');
        $this->request->db->do("insert into UserWhatsappIncomingWebhook (Data) values (?)", [$payload]);
    }

    public function partnerDeleteInstance($instanceId)
    {
        $data = $this->sendData('deleteInstanceAccount', [ 'idInstance' => (int)$instanceId ], true);
        if (!isset($data) || $data[1] !== 200)
            return null;
        $data = json_decode($data[0], true);
        if (isset($data['deleteInstanceAccount']))
        {
            return $data['deleteInstanceAccount'];
        }
        return null;
    }

    public function partnerCreateInstance($userId, $periodDays, $serviceId, $userServiceID)
    {
        $tokenUrl = GenerateSessionID(20);
        $requestData = [
            "webhookUrl" => "https://" . $_SERVER['HTTP_HOST'] . "/api/v1/greenApiPartnerWebhook/",
            "webhookUrlToken" => $tokenUrl,
            "delaySendMessagesMilliseconds" => 300,
            "markIncomingMessagesReaded" => "yes",
            "markIncomingMessagesReadedOnReply" => "yes",
            "outgoingAPIMessageWebhook" => "yes",
            "outgoingWebhook" => "yes",
            "outgoingMessageWebhook" => "yes",
            "incomingWebhook" => "yes",
            "deviceWebhook" => "no",
            "stateWebhook" => "yes",
            "keepOnlineStatus" => "no",
            "incomingCallWebhook" => "yes"
        ];

        $data = $this->sendData('createInstance', $requestData, true);
        if (!isset($data) || $data[1] !== 200)
            return null;

        $data = json_decode($data[0], true);
        if (isset($data['idInstance'], $data['apiTokenInstance']))
        {
            if (!isset($userServiceID))
            {
                $OperationKey = GenerateSessionID(20);
                $this->request->db->do("insert into UserServices (UserID, ServiceID, PaidTillDate, IsActive, OperationKey) values (?, ?, CAST(DATEADD(day, {$periodDays}, GetDate()) AS DATE), 1, ?)", [ $userId, $serviceId, $OperationKey ]);
                $userServiceID = $this->request->db->GetLastID();

                $this->request->db->do("insert into UserWhatsAppInstances (UserID, Domain, AuthToken, InstanceID, Token, PaidTillDate, IsActive, UserServiceID ) values (?, ?, ?, ?, ?, CAST(DATEADD(day, {$periodDays}, GetDate() ) AS DATE), 1, ?)",
                    [ $userId, $this->partnerDomain, $tokenUrl, $data['idInstance'], $data['apiTokenInstance'], $userServiceID ]);
                return $this->request->db->GetLastID();
            }
        }

        return null;
    }

    public function ParseIncomingWebhook($payload, $instanceID, $userID, $doDecode = false, $takeInstanceFromPayload = false)
    {
        if ($doDecode)
        {
            $payload = json_decode($payload, true);
            if (!isset($payload))
                return null;
        }

        if ($takeInstanceFromPayload && isset($payload['instanceData']))
        {
            $instanceData = $this->request->db->select_row_array('SELECT * FROM UserWhatsappInstances WHERE InstanceID = ?', [ $payload['instanceData']['idInstance'] ]);
            if (isset($instanceData[0]))
            {
                $instanceID = $instanceData[0]['ID'];
                $userID = $instanceData[0]['UserID'];
            }
            else
            {
                return null;
            }
        }
        else
        {
            return null;
        }

        if (isset($payload['typeWebhook'], $payload['stateInstance']) && $payload['typeWebhook'] === 'stateInstanceChanged')
        {
            $this->request->db->do("update UserWhatsappInstances set State = ?, StateChangeDate = GetDate() where ID = ?", [ $payload['stateInstance'], $instanceID ]);
            if ($payload['stateInstance'] == 'authorized')
            {
                [$phone, $domain] = explode("@", $payload['instanceData']['wid']);
                $this->request->db->do("update UserWhatsAppInstances set Phone=? where ID=?", [ $phone, $instanceID ]);
            }

            if ($payload['stateInstance'] == 'notAuthorized')
            {
                $this->request->db->do("update UserWhatsAppInstances set Phone=null where ID=?", [ $instanceID ]);
                [$phone, $domain] = explode("@", $payload['instanceData']['wid']);
                $this->request->SendMessageToTelegramToUser($userID, "Номер WhatApp {$phone} отвязан. Настроенные интеграции больше не работают.", null, $instanceID);
            }

            return true;
        }

        if (isset($payload['instanceData'], $payload['typeWebhook'], $payload['timestamp'], $payload['idMessage'],$payload['messageData']))
        {
            $isIncoming = str_starts_with($payload['typeWebhook'], 'incoming');
            $timestamp = $payload['timestamp'];
            $instanceContactId = $this->processWhatsappContact( $payload['instanceData']['wid'], $userID, $instanceID, $isIncoming ? null : $payload['senderData']['senderName'] );
            if (isset($instanceContactId))
            {
                $senderContactId = $this->processWhatsappContact( $payload['senderData']['chatId'], $userID, $instanceID, $payload['senderData']['chatName'], $payload['senderData']['chatName'], $payload['senderData']['chatName'] );
                if (isset($senderContactId))
                {
                    $this->processWhatsappMessage($instanceID,$userID,$payload['idMessage'],$instanceContactId,$senderContactId,$payload['messageData'],$timestamp, $payload['typeWebhook'], $payload['status'] ?? null);
                    return true;
                }
            }
        }
        elseif(isset($payload['instanceData'], $payload['typeWebhook'], $payload['timestamp'], $payload['idMessage'],$payload['status']))
        {
            $this->processWhatsappMessage($instanceID,$userID,$payload['idMessage'],null,null,$payload['messageData'] ?? null,$payload['timestamp'], $payload['typeWebhook'], $payload['status']);
            return true;
        }
        return false;
    }

    private function processWhatsappContact($whatsappId, $userId, $instanceId, $name = null, $chatName = null, $contactName = null): ?int
    {
        $phone = onlydigits($whatsappId);
        if (empty($phone))
            return null;

        $contactData = $this->request->db->select_row_array('SELECT * FROM WhatsappContacts WHERE Phone = ?', [ $phone ]);
        if (!isset($contactData[0]))
        {
            $this->request->db->do("INSERT INTO WhatsappContacts (Phone, Name, IsActive) VALUES (?, ?, 1)", [ $phone, $name ]);
            $contactId = $this->request->db->GetLastID();
        }
        else
        {
            $contactId = $contactData[0]['ID'];
            if (!empty($name) && $name !== $contactData[0]['Name'] && !$this->isOurSenderName($name))
            {
                $this->request->db->do("UPDATE WhatsappContacts SET Name = ? WHERE ID = ?", [$name, $contactId]);
            }
        }

        $userContactData = $this->request->db->select_row_array('SELECT * FROM UserWhatsappContacts WHERE ContactID = ? AND UserID = ? AND InstanceID = ?', [ $contactId, $userId, $instanceId ]);
        if (isset($userContactData[0]))
        {
            if ($chatName !== $userContactData[0]['ChatName'] && !empty($chatName) && !$this->isOurSenderName($chatName))
            {
                $this->request->db->do("UPDATE UserWhatsappContacts SET ChatName = ? WHERE ContactID = ? AND UserID = ? AND InstanceID = ?", [$chatName, $contactId, $userId, $instanceId]);
            }

            if ($contactName !== $userContactData[0]['SenderContactName'] && !empty($contactName) && !$this->isOurSenderName($contactName))
            {
                $this->request->db->do("UPDATE UserWhatsappContacts SET SenderContactName = ? WHERE ContactID = ? AND UserID = ? AND InstanceID = ?", [$contactName, $contactId, $userId, $instanceId]);
            }

            return $userContactData[0]['ID'];
        }

        $this->request->db->do("INSERT INTO UserWhatsappContacts (ContactID, UserID, InstanceID, ChatName, SenderContactName) VALUES (?, ?, ?, ?, ?)", [ $contactId, $userId, $instanceId, $chatName, $contactName]);
        return $this->request->db->GetLastID();
    }

    public function isOurSenderName($name): bool
    {
        return str_starts_with(strtolower($name), 'profitbot');
    }

    private function processWhatsappMessage($instanceID,$userID,$idMessage, $instanceContactId, $senderContactId, $messageData, $timestamp, $typeWebhook, $status): void
    {
        if ($typeWebhook === 'outgoingMessageStatus')
        {
            if (isset($status) && in_array($status, ['sent', 'delivered', 'read']))
            {
                $statusId = 1;
                if ($status === 'delivered') $statusId = 2;
                if ($status === 'read') $statusId = 3;

                $this->request->db->do("UPDATE UserWhatsappMessages SET StatusID = ? WHERE MessageID = ? AND InstanceID = ?", [$statusId, $idMessage, $instanceID]);
            }

            return;
        }

        if (!isset($instanceID, $idMessage, $instanceContactId, $senderContactId, $messageData, $timestamp, $typeWebhook))
            return;

        $messageType = $messageData['typeMessage'];
        $isIncoming = str_starts_with($typeWebhook, 'incoming');
        $isAPIMessage = str_starts_with($typeWebhook, 'outgoingAPI');

        if ($messageType === 'textMessage')
        {
            $messageText = $messageData['textMessageData']['textMessage'];
            $this->request->db->do("INSERT INTO UserWhatsappMessages (InstanceID,UserContactID, MessageID, Message, SentDate, IsIncoming) VALUES (?, ?, ?, ?, ?, ?)", [
                $instanceID, $senderContactId, $idMessage,$messageText, timeToSQL($timestamp), $isIncoming ? 1 : 0
            ]);
        }
        elseif ($messageType === 'quotedMessage')
        {
            $messageText = $messageData['extendedTextMessageData']['text'];
            $stanzaId = $messageData['extendedTextMessageData']['stanzaId'] ?? null;
            $quotedMessageId = $messageData['quotedMessage']['stanzaId'] ?? null;
            $quotedContactId = $this->processWhatsappContact( $messageData['quotedMessage']['participant'], $userID, $instanceID );
            $quotedText = null;
            if ($messageData['quotedMessage']['typeMessage'] === 'textMessage')
            {
                $quotedText = $messageData['quotedMessage']['textMessage'];
            }
            $this->request->db->do("INSERT INTO UserWhatsappMessages (InstanceID,UserContactID, MessageID, Message, SentDate, IsIncoming, QuotedMessageID, QuotedContactID, QuotedMessage) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", [
                $instanceID, $senderContactId, $idMessage,$messageText, timeToSQL($timestamp), $isIncoming ? 1 : 0, $quotedMessageId, $quotedContactId, $quotedText
            ]);
        }
        elseif ($messageType === 'extendedTextMessage')
        {
            $messageText = $messageData['extendedTextMessageData']['text'];
            $this->request->db->do("INSERT INTO UserWhatsappMessages (InstanceID,UserContactID, MessageID, Message, SentDate, IsIncoming, Description, Title, PreviewType, jpegThumbnail, ForwardingScore, IsForwarded ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", [
                $instanceID, $senderContactId, $idMessage,$messageText, timeToSQL($timestamp), $isIncoming ? 1 : 0,$messageData['extendedTextMessageData']['description'] ?? null,$messageData['extendedTextMessageData']['title'] ?? null,
                $messageData['extendedTextMessageData']['previewType'] ?? null, $messageData['extendedTextMessageData']['jpegThumbnail'] ?? null, $messageData['extendedTextMessageData']['forwardingScore'] ?? 0, $messageData['extendedTextMessageData']['isForwarded'] ?? 0
            ]);
        }
        elseif (in_array($messageType, ['documentMessage', 'audioMessage', 'imageMessage']))
        {
            $this->request->db->do("INSERT INTO UserWhatsappMessages (InstanceID,UserContactID, MessageID, Message, SentDate, IsIncoming, DownloadUrl, Caption, FileName, jpegThumbnail, ForwardingScore, IsForwarded, IsAnimated, MimeType ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", [
                $instanceID, $senderContactId, $idMessage, '',  timeToSQL($timestamp), $isIncoming ? 1 : 0,$messageData['fileMessageData']['downloadUrl'] ?? null,$messageData['fileMessageData']['caption'] ?? null,
                $messageData['fileMessageData']['fileName'] ?? null, $messageData['fileMessageData']['jpegThumbnail'] ?? null, $messageData['fileMessageData']['forwardingScore'] ?? 0,
                $messageData['fileMessageData']['isForwarded'] ?? 0, $messageData['fileMessageData']['isAnimated'] ?? 0, $messageData['fileMessageData']['mimeType'] ?? null
            ]);
        }
        elseif ($messageType === 'locationMessage')
        {
            $this->request->db->do("INSERT INTO UserWhatsappMessages (InstanceID,UserContactID, MessageID, Message, SentDate, IsIncoming, NameLocation, Address, jpegThumbnail, Latitude, Longitude, IsForwarded, ForwardingScore ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", [
                $instanceID, $senderContactId, $idMessage, '',  timeToSQL($timestamp), $isIncoming ? 1 : 0,$messageData['locationMessageData']['nameLocation'] ?? null,$messageData['locationMessageData']['address'] ?? null,
                $messageData['locationMessageData']['jpegThumbnail'] ?? null, $messageData['locationMessageData']['latitude'] ?? null, $messageData['locationMessageData']['longitude'] ?? 0,
                $messageData['locationMessageData']['isForwarded'] ?? 0, $messageData['locationMessageData']['forwardingScore'] ?? 0
            ]);
        }
    }
}