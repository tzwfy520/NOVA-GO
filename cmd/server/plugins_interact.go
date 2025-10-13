package main

// 引入交互平台插件，触发各平台的 init() 完成注册
import (
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/interact/platforms/cisco_ios"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/interact/platforms/huawei_s"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/interact/platforms/huawei_ce"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/interact/platforms/h3c_s"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/interact/platforms/h3c_sr"
    _ "github.com/sshcollectorpro/sshcollectorpro/addone/interact/platforms/h3c_msr"
)