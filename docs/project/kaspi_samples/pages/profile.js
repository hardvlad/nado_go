$("[name= 'validation-phone']").mask("+7 (000) 000-00-00");
$("[name= 'marketinglogin']").mask("+7 (000) 000-00-00");
$("[name= 'validation-shopPhone']").mask("+7 (000) 000-00-00");
let deleteShopId = 0;
let deleteStoreId = 0;
let selectMerchantModelMode = null;
let selectedMerchantID = null;

$('#addKaspiShopForm').validate({
    focusInvalid: false,
    rules: {
        'validation-shopname': {
            required: true,
        },
        'validation-taxpercent': {
            required: true,
        },
        'validation-apikey': {
            required: true,
        },
    },
    errorPlacement: function errorPlacement(error, element) {
        $(element).siblings(".validation-error").removeClass("d-none");
        if (error[0].textContent === "Please enter the same value again.") {
            $(element).siblings(".validation-error").text("Password Mismatch")
        }
        return true
    },
    highlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.addClass("invalid-field")
    },
    unhighlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.removeClass("invalid-field");
        $(element).siblings(".validation-error").addClass("d-none")
    },
    submitHandler: function (form) {
        callAddKaspiShop()
    }
});

function callAddKaspiShop()
{
    $("#kaspi-shop-checking").show();
    $("#error-lk-creds").hide();
    $("#error-api-key").hide();
    $("#profileSaveButton").hide();
    callAPI('addKaspiShop',
        {
            name:$("#shopname").val(),
            apikey:$("#apikey").val(),
            id:$("#shopid").val(),
            lkemail:$("#lkemail").val(),
            lkpassword:$("#lkpassword").val(),
            taxpercent:$("#taxpercent").val(),
            marketinglogin:$("#marketinglogin").val(),
            marketingpassword:$("#marketingpassword").val(),
            dosendprice:$('#dosendprice').prop('checked') ? 1 : 0,
            selectedMerchantID: selectedMerchantID
    }, successFunction, errorFunction);
}

function selectMerchant()
{
    selectedMerchantID = $("#select-merchant-select").val();
    $("#select-merchant-modal").modal('hide');

    if (selectMerchantModelMode === 1)
        callAddKaspiShop();
    else
    {
        callAPI('shopOTPLogin',{ action:'selectMerchant', id:otpLoginRecordID, merchantID:selectedMerchantID },
            function(data) {
                $("#alert_checking").html("Мерчант выбран, производится проверка...");
                startCheckingCheckStatus();
            },
            function(data) {
                $("#alert_checking").hide();
                $("#alert_text_check").html("Возникла ошибка. Попробуйте обновить страницу и добавить магазин заново.");
                $("#error_alert_check").show();
            }
        );
    }
}

function successFunction(data)
{
    $("#kaspi-shop-checking").hide();
    $("#profileSaveButton").show();

    console.log(data);
    $('#kaspi_shops_card_div').html(data.data.page);
    $('#add-kaspi-modal').modal('hide');
}

function errorFunction(data)
{
    $("#kaspi-shop-checking").hide();
    $("#profileSaveButton").show();

    if (data.responseJSON.needSelectMerchant === true)
    {
        $("#select-merchant-select").html(data.responseJSON.merchants);
        $("#select-merchant-modal").modal('show');
        selectMerchantModelMode = 1;
        return;
    }


    if (data.responseJSON.credsValid === false)
    {
        $("#error-lk-creds").show();
    }

    if (data.responseJSON.marketingCredsValid === false)
    {
        $("#error-marketing-creds").show();
    }

    if (data.responseJSON.tokenValid === false)
    {
        $('#apikeygroup').show();
        $("#error-api-key").show();
    }

    if (data.responseJSON.isValid === false)
    {
        $("#email").siblings(".validation-error").removeClass("d-none");
    }
    else
    if (data.responseJSON.isExisted === true)
    {
        const myModal= new bootstrap.Modal("#warning-alert-modal");
        myModal.show();
    }
}

function successGetShopFunction(data)
{
    $("#shopname").val(data.data.name);
    $("#apikey").val(data.data.token);
    $('#shopid').val(data.data.id);
    $("#lkemail").val(data.data.lkemail);
    $("#lkpassword").val(data.data.lkpassword);
    $("#marketinglogin").val(data.data.marketinglogin);
    $("#marketingpassword").val(data.data.marketingpassword);
    $("#taxpercent").val(data.data.taxpercent);
    $('#dosendprice').prop('checked', (data.data.dosendprice == 1));
    $('#kaspi-store-card').show();
    $('#xmlLink').val(data.data.xmlLink);
    $('#view_kaspi_storelist_tbody').html(data.data.storesTable);
    $('#kaspiShopLinkDiv').show();
    $('#kaspiSendPriceDiv').show();
    $("#error-marketing-creds").hide();
    $('#add-kaspi-modalLabel').html('Редактировать Каспи магазин')

    $('#login-phone-link').removeClass('active');
    $('#login-phone').removeClass('active');
    $('#login-email-link').addClass('active');
    $('#login-email').addClass('active');

    $('#apikeygroup').show();
    $('#marketinglogingroup').show();
    $('#marketingpasswordgroup').show();

    selectedMerchantID = data.data.merchantID;

    $('#add-kaspi-modal').modal('show',{backdrop: 'static', keyboard: false});
}

function errorGetShopFunction() {

}

function editKaspiShop(id)
{
    callAPI('getKaspiShop', {id:id}, successGetShopFunction, errorGetShopFunction);
}

function addKaspiShop()
{
    $("#shopname").val("");
    $("#apikey").val("");
    $("#lkemail").val("");
    $("#lkpassword").val("");
    $("#marketinglogin").val("");
    $("#marketingpassword").val("");
    $('#shopid').val(0);
    $('#kaspi-store-card').hide();
    $('#view_kaspi_storelist_tbody').html('');
    $('#add-kaspi-modalLabel').html('Добавить Каспи магазин')
    $('#kaspiShopLinkDiv').hide();
    $('#kaspiSendPriceDiv').hide();
    $('#dosendprice').prop('checked', false);

    $('#apikeygroup').hide();
    $('#marketinglogingroup').hide();
    $('#marketingpasswordgroup').hide();
    $('#taxpercent').val(3);

    $('#login-phone-link').addClass('active');
    $('#login-phone').addClass('active');
    $('#login-email-link').removeClass('active');
    $('#login-email').removeClass('active');

    selectedMerchantID = null;

    $('#add-kaspi-modal').modal('show');
}

function deleteKaspiShop(id)
{
    deleteShopId = id;
    $('#delete-kaspi-modal').modal('show');
}

function successDeleteShopFunction(data)
{
    $('#delete-kaspi-modal').modal('hide');
    $('#kaspi_shops_card_div').html(data.data.page);
}

function errorDeleteShopFunction()
{

}

function doDeleteKaspiShop()
{
    callAPI('deleteKaspiShop', {id:deleteShopId}, successDeleteShopFunction, errorDeleteShopFunction);
}

function successSaveProfileFunction()
{
    $('#saveProfile-success-modal').modal('show');
}

function errorSaveProfileFunction()
{
    $('#saveProfile-alert-modal').modal('show');
}

$('#addprofileform').validate({
    focusInvalid: false,
    rules: {
        'validation-firstname': {
            required: false,
        },
        'validation-lastname': {
            required: false,
        },
        'validation-phone': {
            required: false,
            minlength: 18,
//            depends: function(element) {
//                return $("[name= 'validation-phone']").mask("isComplete");
//            }
        },
    },
    errorPlacement: function errorPlacement(error, element) {
        $(element).siblings(".validation-error").removeClass("d-none");
        if (error[0].textContent === "Please enter the same value again.") {
            $(element).siblings(".validation-error").text("Password Mismatch")
        }
        return true
    },
    highlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.addClass("invalid-field")
    },
    unhighlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.removeClass("invalid-field");
        $(element).siblings(".validation-error").addClass("d-none")
    },
    submitHandler: function (form) {
        callAPI('saveProfile', {firstname:$("#fname").val(),lastname:$("#lname").val(),phone:$("#tel").val()}, successSaveProfileFunction, errorSaveProfileFunction);
    }
});

function successStoreFunction(data)
{
    $('#view_kaspi_storelist_tbody').html(data.data.storesTable);
    $('#add-kaspi-store-modal').modal('hide');
}

function errorStoreFunction() {

}

$('#addKaspiShopStoreForm').validate({
    focusInvalid: false,
    rules: {
        'validation-storecode': {
            required: true,
        },
        'validation-storename': {
            required: true,
        },
    },
    errorPlacement: function errorPlacement(error, element) {
        $(element).siblings(".validation-error").removeClass("d-none");
        if (error[0].textContent === "Please enter the same value again.") {
            $(element).siblings(".validation-error").text("Password Mismatch")
        }
        return true
    },
    highlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.addClass("invalid-field")
    },
    unhighlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.removeClass("invalid-field");
        $(element).siblings(".validation-error").addClass("d-none")
    },
    submitHandler: function (form) {
        callAPI('addKaspiShopStore', {name:$("#storename").val(),code:$("#storecode").val(),id:$("#storeid").val(),shopid:$("#shopid").val(),address:$("#storeaddress").val(),addresslink:$("#storeaddresslink").val()}, successStoreFunction, errorStoreFunction);
    }
});

function addKaspiStore()
{
    $("#storecode").val("");
    $("#storename").val("");
    $('#storeid').val(0);
    $('#add-kaspi-store-modalLabel').html('Добавить склад каспи магазина')
    $('#add-kaspi-store-modal').modal('show');
}

function successGetStoreFunction(data) {
    $("#storecode").val(data.data.code);
    $("#storename").val(data.data.name);
    $('#storeid').val(data.data.id);
    $("#storeaddress").val(data.data.address);
    $("#storeaddresslink").val(data.data.addresslink);

    $('#add-kaspi-store-modalLabel').html('Редактировать склад каспи магазина')
    $('#add-kaspi-store-modal').modal('show');
}

function errorGetStoreFunction() {

}

function editKaspiStore(id)
{
    callAPI('getKaspiShopStore', {id:id}, successGetStoreFunction, errorGetStoreFunction);
}

function deleteKaspiStore(id)
{
    deleteStoreId = id;
    $('#delete-kaspi-store-modal').modal('show');
}

function successDeleteStoreFunction(data)
{
    $('#delete-kaspi-store-modal').modal('hide');
    $('#view_kaspi_storelist_tbody').html(data.data.storesTable);
}

function errorDeleteStoreFunction() {

}

function doDeleteKaspiShopStore()
{
    callAPI('deleteKaspiShopStore', {id:deleteStoreId}, successDeleteStoreFunction, errorDeleteStoreFunction);
}


function referralUsersList()
{
    itemsTable = $("#user_table_list").DataTable(
        {
            ajax: '/api/v1/referralUsersList',
            columns: [
                { data: 'regdate' },
                { data: 'name' } ,
                { data: 'email' },
                { data: 'phone' },
                { data: 'shopscount' },
                { data: 'paysum' },
                { data: 'pbtilldate' },
                { data: 'watilldate' },
            ],
            drawCallback:function()
            {
//                $(".dataTables_paginate > .pagination").addClass("flat-rounded-pagination ");
            },
            language:
                {
                    paginate:{first:"«",last:"»",previous:"<i class='bx bx-chevron-left'>",next:"<i class='bx bx-chevron-right'>"},
                    emptyTable:     "Данные отсутствуют в таблице",
                    info:           "Отображаются строки с _START_ по _END_ из всего _TOTAL_ строк",
                    infoEmpty:      "Данные отсутствуют в таблице",
                    lengthMenu:     "Показать _MENU_ строк",
                    loadingRecords: "Загрузка...",
                },
            processing: true,
            serverSide: true,
            pagingType: 'full_numbers',

            initComplete: function (settings, json)
            {
                $('#user_table_list').attr('style','');
            }
        }
    );
}

function referralPaymentsList()
{
    itemsTable = $("#payments_table_list").DataTable(
        {
            ajax: '/api/v1/referralPaymentsList',
            columns: [
                { data: 'date' },
                { data: 'sum' } ,
                { data: 'type' },
            ],
            drawCallback:function()
            {
//                $(".dataTables_paginate > .pagination").addClass("flat-rounded-pagination ");
            },
            language:
                {
                    paginate:{first:"«",last:"»",previous:"<i class='bx bx-chevron-left'>",next:"<i class='bx bx-chevron-right'>"},
                    emptyTable:     "Данные отсутствуют в таблице",
                    info:           "Отображаются строки с _START_ по _END_ из всего _TOTAL_ строк",
                    infoEmpty:      "Данные отсутствуют в таблице",
                    lengthMenu:     "Показать _MENU_ строк",
                    loadingRecords: "Загрузка...",
                },
            processing: true,
            serverSide: true,
            pagingType: 'full_numbers',

            initComplete: function (settings, json)
            {
                $('#payments_table_list').attr('style','');
            }
        }
    );
}

function showAlert(text)
{
    $("#alert_text").html(text);
    $("#error_alert").show();
}

$('#changepasswordform').validate({
    focusInvalid: false,
    rules: {
        'validation-newpassword': {
            required: true,
            minlength: 6
        },
    },
    errorPlacement: function errorPlacement(error, element) {
        $(element).siblings(".validation-error").removeClass("d-none");
        if (error[0].textContent === "Please enter the same value again.") {
            $(element).siblings(".validation-error").text("Password Mismatch")
        }
        return true
    },
    highlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.addClass("invalid-field")
    },
    unhighlight: function (element) {
        var $el = $(element);
        var $parent = $el.parents('.form-group');
        $parent.removeClass("invalid-field");
        $(element).siblings(".validation-error").addClass("d-none")
    },
    submitHandler: function (form) {
        $("#error_alert").hide();
        callAPI('changePassword', {password:$("#newpassword").val(),logoffall:$('#logoffall').prop('checked') ? 1 : 0}, function(data){
            $('#changePasswordSuccess').show();
            setTimeout(function() {
                $('#changePasswordSuccess').hide();
            }, 2000);

        }, function(data){
            showAlert(data.responseJSON.errorMessage);
        });
    }
});

let otpLoginRecordID=0;

function startCheckingSendStatus()
{
    setTimeout(function()
    {
        callAPI('shopOTPLogin',{action:'sendStatus',id:otpLoginRecordID},
            function(data)
            {
                if (data.data.status == null)
                {
                    startCheckingSendStatus();
                }

                if (data.data.status == 0)
                {
                    $("#alert_sending").hide();
                    $("#otp_send_button_div").show();
                    $("#alert_text_send").html("Не удалось отправить код. Проверьте номер телефона и попробуйте ещё раз.");
                    $("#error_alert_send").show();
                }

                if (data.data.status == 1)
                {
                    $("#alert_sending").hide();
                    $("#otp_send_button_div").hide();
                    $("#otpCodeDiv").show();
                }
            },
            function(data)
            {
                $("#alert_sending").hide();
                $("#otp_send_button_div").show();
                $("#alert_text_send").html(data.responseJSON.errorMessage);
                $("#error_alert_send").show();
            });
    }, 1000);
}

function sendOTPCode()
{
    $("#alert_sending").hide();
    $("#error_alert_send").hide();
    callAPI('shopOTPLogin', {action:'send',phone:$('#shopPhone').val()},
        function(data)
        {
            $("#otp_send_button_div").hide();
            $("#alert_sending").show();
            otpLoginRecordID = data.data.id;
            startCheckingSendStatus();
        },
        function(data)
        {
            $("#alert_sending").hide();
            $("#otp_send_button_div").show();
            $("#alert_text_send").html(data.responseJSON.errorMessage);
            $("#error_alert_send").show();
        }
    );
}

function startCheckingCheckStatus()
{
    setTimeout(function()
    {
        callAPI('shopOTPLogin',{action:'checkStatus',id:otpLoginRecordID},
            function(data)
            {
                if (data.data.status == null)
                {
                    startCheckingCheckStatus();
                }

                if (data.data.status == 2)
                {
                    $("#select-merchant-select").html(data.data.merchants);
                    $("#select-merchant-modal").modal('show');
                    $("#alert_checking").html("Выберите мерчанта");
                    selectMerchantModelMode = 2;
                }

                if (data.data.status == 0)
                {
                    $("#alert_checking").hide();
                    $("#otp_check_button_div").show();
                    $("#alert_text_check").html("Не удалось подключить магазин.<br>Возможно, код неверный или у аккаунта недостаточно прав.<br><br>Если код указан верно, добавьте доступ через email по инструкции:<br><a href='https://profitbot.kz/faq_login'>https://profitbot.kz/faq_login</a>");
                    $("#error_alert_check").show();
                }

                if (data.data.status == 1)
                {
                    $("#alert_checking").hide();
                    $("#otp_check_button_div").hide();
                    $("#alert_check_success").show();

                    if (data.data.password == null)
                    {
                        startCheckingCheckStatus();
                    }
                    else
                    {
                        $('#alert_check_success').hide();
                        $('#alert_check_finished').show();
                        $('#lkemail').val(data.data.email);
                        $('#lkpassword').val(data.data.password);
                        $('#apikey').val(data.data.token != null ? data.data.token : '');
                    }
                }
            },
            function(data)
            {
                $("#alert_checking").hide();
                $("#otp_check_button_div").show();
                $("#alert_text_check").html(data.responseJSON.errorMessage);
                $("#error_alert_check").show();
            });
    }, 1000);
}

function checkOTPCode()
{
    $("#alert_checking").hide();
    $("#error_alert_check").hide();
    callAPI('shopOTPLogin', {action:'check',otp:$('#otpcode').val(),id:otpLoginRecordID},
        function(data)
        {
            $("#otp_check_button_div").hide();
            $("#alert_checking").show();
            startCheckingCheckStatus();
        },
        function(data)
        {
            $("#alert_checking").hide();
            $("#otp_check_button_div").show();
            $("#alert_text_check").html(data.responseJSON.errorMessage);
            $("#error_alert_check").show();
        }
    );
}

function escapeHtml(value)
{
    return $('<div>').text(value ?? '').html();
}

function getMcpServerUrl()
{
    return 'https://my.profitbot.kz/mcp';
}

function buildMcpConfigJson(token)
{
    const config = {
        mcpServers: {
            profitbot: {
                command: 'npx',
                args: ['-y', 'mcp-remote', getMcpServerUrl(), '--header', 'Authorization: Bearer ' + token]
            }
        }
    };
    return JSON.stringify(config, null, 2);
}

function updateMcpConfigSnippet(token)
{
    $('#mcp-config-json').text(buildMcpConfigJson(token || '<ваш_токен>'));
}

function copyMcpConfig()
{
    const value = $('#mcp-config-json').text();
    if (!value) return;
    navigator.clipboard.writeText(value);
}

function initMcpConnectionInfo()
{
    $('#mcp-server-url').text(getMcpServerUrl());
    $('#mcp-server-url-2').text(getMcpServerUrl());
    updateMcpConfigSnippet();
}

function generateMcpToken()
{
    const label = window.prompt('Название токена (например, Claude Desktop):', 'Claude Desktop');
    if (label === null) return;
    callAPI('generateMcpToken', {label: label}, successGenerateMcpTokenFunction, errorGenerateMcpTokenFunction);
}

function successGenerateMcpTokenFunction(data)
{
    $('#mcp-new-token-value').val(data.data.token);
    $('#mcp-new-token-alert').show();
    updateMcpConfigSnippet(data.data.token);
    loadMcpTokens();
}

function errorGenerateMcpTokenFunction()
{
}

function copyMcpToken()
{
    const value = $('#mcp-new-token-value').val();
    if (!value) return;
    navigator.clipboard.writeText(value);
}

function loadMcpTokens()
{
    callAPI('listMcpTokens', {}, successListMcpTokensFunction, function() {});
}

function successListMcpTokensFunction(data)
{
    const tbody = $('#mcp_tokens_tbody');
    tbody.html('');
    (data.data || []).forEach(function(token)
    {
        const status = token.isRevoked ? '<span class="badge bg-danger">Отозван</span>' : '<span class="badge bg-success">Активен</span>';
        const lastUsed = token.lastUsedDate ? escapeHtml(token.lastUsedDate) : '—';
        const action = token.isRevoked ? '' : '<button type="button" class="btn btn-sm btn-outline-danger" onclick="revokeMcpToken(' + token.id + ')">Отозвать</button>';
        tbody.append(
            '<tr><td>' + escapeHtml(token.label || '—') + '</td><td>' + escapeHtml(token.createdDate) + '</td><td>' + lastUsed + '</td><td>' + status + '</td><td>' + action + '</td></tr>'
        );
    });
}

function revokeMcpToken(id)
{
    if (!confirm('Отозвать этот токен? AI-ассистент, использующий его, потеряет доступ.')) return;
    callAPI('revokeMcpToken', {id: id}, function() { loadMcpTokens(); }, function() {});
}

$(function()
{
    referralUsersList();
    referralPaymentsList();
    initMcpConnectionInfo();
    loadMcpTokens();
    if (typeof userHasNoKaspiShops !== 'undefined')
    {
        setTimeout(function(){addKaspiShop();}, 100);
    }
}(jQuery));
