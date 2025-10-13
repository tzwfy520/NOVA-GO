package main

// 引入采集平台插件，触发各平台的 init() 完成注册
import (
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/collect/platforms/cisco_ios"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/collect/platforms/huawei_s"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/collect/platforms/huawei_ce"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/collect/platforms/h3c_s"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/collect/platforms/h3c_sr"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/collect/platforms/h3c_msr"
)