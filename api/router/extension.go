package router

import "github.com/gin-gonic/gin"

// ExtraRoutesFunc 是商业版本或外部模块注入的扩展路由函数。
// 若设置，则在基础路由注册完成后调用，用于挂载额外的 API/UI 路由。
var ExtraRoutesFunc func(r *gin.Engine)

// RegisterExtraRoutes 允许外部在初始化阶段注册扩展路由函数。
func RegisterExtraRoutes(f func(r *gin.Engine)) {
	ExtraRoutesFunc = f
}
