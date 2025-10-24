package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	// 新增：数据库与模型
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"gorm.io/gorm"
	"gopkg.in/yaml.v3"
)

type SimulateConfigHandler struct{}

func NewSimulateConfigHandler() *SimulateConfigHandler { return &SimulateConfigHandler{} }

// 命名空间配置
// 表结构：name(string, unique), port(int), idle_seconds(int), max_conn(int)
type NamespaceConf struct {
	Port        int `yaml:"port" json:"port"`
	IdleSeconds int `yaml:"idle_seconds" json:"idle_seconds"`
	MaxConn     int `yaml:"max_conn" json:"max_conn"`
}

type DeviceTypeConf struct {
	PromptSuffixe      string `yaml:"prompt_suffixe" json:"prompt_suffixe"`
	EnableModeRequired bool   `yaml:"enable_mode_required" json:"enable_mode_required"`
	EnableModeSuffixe  string `yaml:"enable_mode_suffixe,omitempty" json:"enable_mode_suffixe,omitempty"`
	ConfigModeSuffixe  string `yaml:"config_mode_suffixe,omitempty" json:"config_mode_suffixe,omitempty"`
}

// 新增：模拟设备名称到类型的映射
type DeviceNameConf struct {
	DeviceType string `yaml:"device_type" json:"device_type"`
}

type SimulateConfig struct {
	Namespace  map[string]NamespaceConf  `yaml:"namespace" json:"namespace"`
	DeviceType map[string]DeviceTypeConf `yaml:"device_type" json:"device_type"`
	DeviceName map[string]DeviceNameConf `yaml:"device_name" json:"device_name"`
}

// GetSimulateConfig 读取 simulate-auto.yaml（若不存在则回退 simulate.yaml）并以表格友好结构返回
func (h *SimulateConfigHandler) GetSimulateConfig(c *gin.Context) {
	// 优先从SQLite读取（如有数据则直接返回）；否则回退到YAML
	db := database.GetDB()
	var nsRows []model.SimNamespace
	var dtRows []model.SimDeviceType
	var dnRows []model.SimDeviceName
	if err := db.Find(&nsRows).Error; err == nil {
		_ = db.Find(&dtRows).Error
		_ = db.Find(&dnRows).Error
		if len(nsRows)+len(dtRows)+len(dnRows) > 0 {
			// 构建数组返回
			namespaces := make([]gin.H, 0, len(nsRows)+1)
			deviceTypes := make([]gin.H, 0, len(dtRows))
			deviceNames := make([]gin.H, 0, len(dnRows))
			defaultPresent := false
			for _, n := range nsRows {
				if n.Name == "default" { defaultPresent = true }
				namespaces = append(namespaces, gin.H{
					"name": n.Name,
					"port": n.Port,
					"idle_seconds": n.IdleSeconds,
					"max_conn": n.MaxConn,
				})
			}
			if !defaultPresent {
				namespaces = append(namespaces, gin.H{"name": "default", "port": 22001, "idle_seconds": 180, "max_conn": 5})
			}
			for _, d := range dtRows {
				deviceTypes = append(deviceTypes, gin.H{
					"type": d.Type,
					"prompt_suffixe": d.PromptSuffixe,
					"enable_mode_required": d.EnableModeRequired,
					"enable_mode_suffixe": d.EnableModeSuffixe,
					"config_mode_suffixe": d.ConfigModeSuffixe,
				})
			}
			for _, dn := range dnRows {
				deviceNames = append(deviceNames, gin.H{
					"name": dn.Name,
					"device_type": dn.DeviceType,
				})
			}
			c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "OK", "data": gin.H{"namespaces": namespaces, "device_types": deviceTypes, "device_names": deviceNames}})
			return
		}
	}

	autoPath := filepath.Join("simulate", "simulate-auto.yaml")
	path := autoPath
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join("simulate", "simulate.yaml")
	}
	var sc SimulateConfig
	if _, err := os.Stat(path); err == nil {
		b, err := os.ReadFile(path)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "READ_FAILED", "message": "读取失败: " + err.Error()})
			return
		}
		if err := yaml.Unmarshal(b, &sc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "YAML_ERROR", "message": "解析失败: " + err.Error()})
			return
		}
	}
	// 保证默认命名空间存在
	if sc.Namespace == nil { sc.Namespace = make(map[string]NamespaceConf) }
	if _, ok := sc.Namespace["default"]; !ok {
		sc.Namespace["default"] = NamespaceConf{ Port: 22001, IdleSeconds: 180, MaxConn: 5 }
	}
	// 转换为数组，便于前端渲染与编辑
	namespaces := make([]gin.H, 0, len(sc.Namespace))
	for name, n := range sc.Namespace {
		namespaces = append(namespaces, gin.H{
			"name":         name,
			"port":         n.Port,
			"idle_seconds": n.IdleSeconds,
			"max_conn":     n.MaxConn,
		})
	}
	deviceTypes := make([]gin.H, 0, len(sc.DeviceType))
	for typ, d := range sc.DeviceType {
		deviceTypes = append(deviceTypes, gin.H{
			"type":                 typ,
			"prompt_suffixe":       d.PromptSuffixe,
			"enable_mode_required": d.EnableModeRequired,
			"enable_mode_suffixe":  d.EnableModeSuffixe,
			"config_mode_suffixe":  d.ConfigModeSuffixe,
		})
	}
	// 新增：设备名称映射数组
	deviceNames := make([]gin.H, 0, len(sc.DeviceName))
	for name, dn := range sc.DeviceName {
		deviceNames = append(deviceNames, gin.H{
			"name":        name,
			"device_type": dn.DeviceType,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "OK", "data": gin.H{"namespaces": namespaces, "device_types": deviceTypes, "device_names": deviceNames}})
}

// SaveSimulateConfig 保存模拟配置（带事务与重试）
func (h *SimulateConfigHandler) SaveSimulateConfig(c *gin.Context) {
	var payload SimulateConfig
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数错误: " + err.Error()})
		return
	}
	// 基本校验（允许 device_name 为空，便于先配置类型）
	if len(payload.Namespace) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "namespace 不能为空"})
		return
	}
	if len(payload.DeviceType) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "device_type 不能为空"})
		return
	}
	// device_name 可为空；如有则在后续引用检查中校验
	// 设备名称引用检查
	for name, dn := range payload.DeviceName {
		if dn.DeviceType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": fmt.Sprintf("设备名称 %s 未指定设备类型", name)})
			return
		}
		if _, ok := payload.DeviceType[dn.DeviceType]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "TYPE_NOT_DEFINED", "message": fmt.Sprintf("设备名称 %s 的设备类型未在 device_type 定义: %s", name, dn.DeviceType)})
			return
		}
	}
	// 先写入SQLite（事务替换整个配置集，带重试）
	if err := database.TransactionWithRetry(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM sim_device_names").Error; err != nil { return err }
		if err := tx.Exec("DELETE FROM sim_device_types").Error; err != nil { return err }
		if err := tx.Exec("DELETE FROM sim_namespaces").Error; err != nil { return err }
		for name, n := range payload.Namespace {
			row := model.SimNamespace{ Name: name, Port: n.Port, IdleSeconds: n.IdleSeconds, MaxConn: n.MaxConn }
			if err := tx.Create(&row).Error; err != nil { return err }
		}
		for typ, d := range payload.DeviceType {
			row := model.SimDeviceType{ Type: typ, PromptSuffixe: d.PromptSuffixe, EnableModeRequired: d.EnableModeRequired, EnableModeSuffixe: d.EnableModeSuffixe, ConfigModeSuffixe: d.ConfigModeSuffixe }
			if err := tx.Create(&row).Error; err != nil { return err }
		}
		for name, dn := range payload.DeviceName {
			row := model.SimDeviceName{ Name: name, DeviceType: dn.DeviceType }
			if err := tx.Create(&row).Error; err != nil { return err }
		}
		return nil
	}, 6, 100*time.Millisecond); err != nil {
		logger.Error("Save simulate config to SQLite failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "保存到数据库失败: " + err.Error()})
		return
	}

	// 同步写入 simulate-auto.yaml（保持现有模拟服务使用 YAML）
	b, err := yaml.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "YAML_ERROR", "message": "序列化失败: " + err.Error()})
		return
	}
	dir := "simulate"
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "MKDIR_FAILED", "message": "创建目录失败: " + err.Error()})
		return
	}
	path := filepath.Join(dir, "simulate-auto.yaml")
	if err := os.WriteFile(path, b, 0644); err != nil {
		logger.Error("Write simulate-auto.yaml failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "WRITE_FAILED", "message": "写入失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "生成成功", "data": gin.H{"path": path}})
}