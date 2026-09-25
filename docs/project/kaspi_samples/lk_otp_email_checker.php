<?php

require_once __DIR__ . DIRECTORY_SEPARATOR . '..' . DIRECTORY_SEPARATOR . 'vendor' . DIRECTORY_SEPARATOR . 'autoload.php';
require_once __DIR__ . DIRECTORY_SEPARATOR . '..' . DIRECTORY_SEPARATOR . 'data' . DIRECTORY_SEPARATOR . 'Modules' . DIRECTORY_SEPARATOR . 'Common.php';

use Dotenv\Dotenv;
use ProfitBot\Modules\ExternalApi\WhatsappOrderMessage;
use ProfitBot\Modules\Http\Request;
use ProfitBot\Modules\ExternalApi\GreenAPI;

checkrun();

$dotenv = Dotenv::createMutable(__DIR__ . DIRECTORY_SEPARATOR . '..' . DIRECTORY_SEPARATOR);
$dotenv->load();

$request = new Request(true, 1);
$GLOBALS['CoreObject'] = $request;

$startTime = time();

while (true)
{
    try
    {
        connectToImapAndProcess($request);
    }
    catch (Exception $e)
    {
        echo $e->getMessage() . "\n";
    }
    sleep(1);

    if (time() - $startTime > 3600)
    {
        break;
    }
}

function connectToImapAndProcess($request): void
{
    $data = $request->db->select_row_array("select ID, Email from UserKaspiShopsOTPRegistration where UserCreateResult=1 and Password IS NULL");
    $emailNoPass = [];
    foreach ($data ?? [] as $item)
    {
        $emailNoPass[strtolower($item['Email'])]  = $item['ID'] ;
    }

    $username = 'service@service.pbot.kz';
    $password = 'Aa712015!';
    $mailbox = '{localhost:993/imap/ssl/novalidate-cert/debug}INBOX';

    $inbox = @imap_open($mailbox, $username, $password);
    if ($inbox === false)
    {
        throw new Exception("Cannot connect to IMAP: " . imap_last_error());
    }

    $emails = imap_search($inbox, 'UNSEEN');
    $nm = $emails ? count($emails) : 0;

//    print "Total messages: $nm\n";

    if ($nm > 0)
    {
        foreach ($emails as $email_number)
        {
            $overview = imap_fetch_overview($inbox, $email_number, 0);

            $from = $overview[0]->from;
            $to = $overview[0]->to;
            $subject = $overview[0]->subject;

            if (!$overview[0]->seen)
            {
                $message = base64_decode(imap_body($inbox, $email_number));

                $pos = stripos($message, 'Ваш код подтверждения: ');
                if ($pos === false)
                {
                    $lines = explode("<br>", $message);
                    $email = null;
                    $password = null;

                    foreach ($lines as $row)
                    {
                        if (str_starts_with($row, 'Логин: '))
                        {
                            $email = trim(substr($row, strlen('Логин: ')));
                        }
                        if (str_starts_with($row, 'Пароль: '))
                        {
                            $password = trim(substr($row, strlen('Пароль: ')));
                        }
                    }

                    if (isset($email) && isset($password) && isset($emailNoPass[strtolower($email)]))
                    {
                        $ID = $emailNoPass[strtolower($email)];
                        $request->db->do("update UserKaspiShopsOTPRegistration set Password=?,PasswordSetDate=GETDATE() where ID=?", [ $password, $ID ]);
                    }
                }
                else
                {
                    $code = substr($message, $pos + strlen('Ваш код подтверждения: '), 6);
                    $code = trim($code);
                    $request->db->do("insert into KaspiEmailOTPCodes (Email, Code) values (?, ?)", [ $to, $code ]);
                }


                imap_setflag_full($inbox, $email_number, "\\Seen");
            }
        }
    }

    imap_close($inbox);
}


function checkrun()
{
    exec("ps auxww", $ps);
    $r = 0;
    foreach ($ps as $p) {
        if (strpos($p, basename(__FILE__))) {
            $r++;
            if ($r > 2) {
                echo "too many instances, exiting\n";
                exit();
            }
        }
    }
}
