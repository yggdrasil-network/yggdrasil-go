<?php

class mesh extends core{
    private static $execute = "/etc/init.d/mesh";

    function _status()
    {
        if(file_exists(self::$execute)){
            $ret = $this->fun->_exec(self::$execute." status");
            if(strstr($ret, "running")){
                return 1;
            }else{
                return 0;
            }
        }else{
            return 0;
        }
    }

    function _switch($stat)
    {
        if($stat == 1){
            $this->_sysAutoEvent(basename(self::$execute), true);
            $this->fun->_backexec(self::$execute." start");
        }else{
            $this->_sysAutoEvent(basename(self::$execute), false);
            $this->fun->_exec(self::$execute." stop");
        }
        sleep(1);
        $ret = $this->_status();
        return $ret;
    }
}
