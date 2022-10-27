<?php
include_once "../../include/app.php";
$mesh = new mesh();
$status = $mesh->_status();
$app_info = $mesh->get_app_info('mesh');
$L = $La->extlanguage("mesh");
$url = "http://".$_SERVER['SERVER_ADDR'].":8181/mesh/";
?>
<!DOCTYPE HTML>
<html>
<!--head>
    <meta http-equiv="Content-Type" content="text/html; charset=utf-8">
    <meta http-equiv="X-UA-Compatible" content="IE=8" />
    <title>mesh</title>
    <link href="../../css/base.css?v=<?=mt_rand()?>" rel="stylesheet" type="text/css">
    <link href="../../css/style-mesh.css?v=<?=mt_rand()?>" rel="stylesheet" type="text/css">
    <script src="../../js/jquery.js"></script>
    <script src="../../js/common.js?v=<?=mt_rand()?>"></script>
    <script>
        $.getScript("/js/lib/cTools.js", function () {
            var ajaxload_callback = new cTools();
        });
    </script>
</head>

<body>
<header class="heder-info">
    <div class="logo-info">
        <figure>
            <img src="../../images/icons/mesh.png">
            <figcaption><?=$L['mesh']?></figcaption>
        </figure>
    </div>
    <div class="v-info">
        <p><span style="display:inline-block;text-align:right;padding-right:10px;"><?php echo $root['meto']['app_version']?>:</span><span><?php echo $app_info['version']?></span></p>
        <p><span style="display:inline-block;text-align:right;padding-right:10px;"><?php echo $root['meto']['app_auth']?>:</span><span><?php echo $app_info['auth']?></span></p>
        <p><span style="display:inline-block;text-align:right;padding-right:10px;"><?php echo $root['meto']['app_publisher']?>:</span><span>TerraMaster</span></p>
    </div>
</header>
<div class="intr">
    <p><?=$L["tos_mesh_note"]?></p>
</div>
<div class="mainbox" style="width: 85%;margin: 0 auto;">
    <div class="conLine">
        <div class="item col-select">
            <input  type="checkbox" name="is_check" mode="tos"  <?php if ($status == 1){echo 'checked';}?> />
        </div>
        <div class="item col-main"><?php echo $root['basic']['basic_enable']?> mesh</div>
    </div>
    <?php if ($status ==1){?>
    <p style="padding-left: 30px;margin-top: 15px;"><button id="_location" class="btn-info-no"><?=$L["enter"]?> mesh</button></p>
    <?php }?>
</div>
<div class="buttons">
    <span class="showAction"></span>
    <button class="button"><?=$root["common"]["save"]?></button>
</div-->
<iframe src="<?=$url?>" width="100%" height="500" align="left" style="
    border-top-width: 0px;
    border-left-width: 0px;
    border-right-width: 0px;
    border-bottom-width: 0px;
">
	 Floating frames is not supported by you browser
</iframe>
<script>
(function(){
    function ajaxrequest(data, callback, obj) {
        $.ajax({type:"POST",url:"/include/core/index.php",data:data, timeout:50000,success: $.proxy(callback, obj)});
    }
    var switch_button = ["<?=$root["basic"]["basic_enable"]?>", "<?=$root["basic"]["basic_disable"]?>"], savebox = [],
        obj = $(".buttons"), status = $('[name=is_check]').prop('checked') ? 1:0, btnbox = obj.find("button");
    btnbox.eq(0).click(function(){
        status = $('[name=is_check]').prop('checked') ? 1:0;
        var show_msg = new waitbox();
        show_msg._init("<?=$root["pt"]["waiting"]?>");
        ajaxrequest({mod:"mesh",act:'_switch',opt:status}, function(ret){
            if(ret == 0){
                $("#manager").hide();
            }else{
                $("#manager").show();
            }
            window.location.reload();
        },this);
    }), status==1 ? $("#manager").show() : $("#manager").hide();

    $("#_location").click(function () {
        window.open('<?php echo $url;?>');
    });
})($);    
</script>
</body>
</html>
