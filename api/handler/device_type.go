package handler

import (
    "net/http"
    "strconv"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/sshcollectorpro/sshcollectorpro/internal/database"
    "github.com/sshcollectorpro/sshcollectorpro/internal/model"
)

// DeviceTypeListResponse 列表返回结构，附带前端展示的类型名称
type DeviceTypeListResponse struct {
    ID       uint   `json:"id"`
    Vendor   string `json:"vendor"`
    System   string `json:"system"`
    Kind     string `json:"kind"`
    Tag      string `json:"tag"`
    SSHType  string `json:"ssh_type"`
    Enabled  bool   `json:"enabled"`
    Name     string `json:"name"` // 厂商+操作系统+类型+标签
}

// ListDeviceTypes GET /api/v1/device-types
func ListDeviceTypes(c *gin.Context) {
    db := database.GetDB()
    var types []model.DeviceType

    q := strings.TrimSpace(c.Query("q"))
    enabledParam := strings.TrimSpace(c.Query("enabled"))

    tx := db.Model(&model.DeviceType{})
    if q != "" {
        like := "%" + q + "%"
        tx = tx.Where("vendor LIKE ? OR system LIKE ? OR kind LIKE ? OR tag LIKE ?", like, like, like, like)
    }
    if enabledParam != "" {
        switch enabledParam {
        case "true", "1":
            tx = tx.Where("enabled = ?", true)
        case "false", "0":
            tx = tx.Where("enabled = ?", false)
        }
    }
    if err := tx.Order("vendor ASC, system ASC, kind ASC, tag ASC").Find(&types).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    res := make([]DeviceTypeListResponse, 0, len(types))
    for _, t := range types {
        name := strings.TrimSpace(strings.Join([]string{t.Vendor, t.System, t.Kind, t.Tag}, "-"))
        res = append(res, DeviceTypeListResponse{
            ID:      t.ID,
            Vendor:  t.Vendor,
            System:  t.System,
            Kind:    t.Kind,
            Tag:     t.Tag,
            SSHType: t.SSHType,
            Enabled: t.Enabled,
            Name:    name,
        })
    }
    c.JSON(http.StatusOK, res)
}

// CreateDeviceType POST /api/v1/device-types
func CreateDeviceType(c *gin.Context) {
    var req model.DeviceType
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
        return
    }
    req.Vendor = strings.TrimSpace(req.Vendor)
    req.System = strings.TrimSpace(req.System)
    req.Kind = strings.TrimSpace(req.Kind)
    req.Tag = strings.TrimSpace(req.Tag)
    req.SSHType = strings.TrimSpace(req.SSHType)
    if req.Tag == "" {
        req.Tag = "default"
    }
    if req.Kind == "" {
        req.Kind = "all"
    }
    if req.Vendor == "" || req.System == "" || req.SSHType == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "vendor, system, ssh_type are required"})
        return
    }

    db := database.GetDB()
    // 组合唯一约束：vendor+system+kind+tag
    var count int64
    if err := db.Model(&model.DeviceType{}).
        Where("vendor = ? AND system = ? AND kind = ? AND tag = ?", req.Vendor, req.System, req.Kind, req.Tag).
        Count(&count).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    if count > 0 {
        c.JSON(http.StatusConflict, gin.H{"error": "device type already exists"})
        return
    }

    if err := db.Create(&req).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"id": req.ID})
}

// GetDeviceType GET /api/v1/device-types/:id
func GetDeviceType(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
        return
    }
    var dt model.DeviceType
    if err := database.GetDB().First(&dt, id).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
        return
    }
    c.JSON(http.StatusOK, dt)
}

// UpdateDeviceType PUT /api/v1/device-types/:id
func UpdateDeviceType(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
        return
    }
    var req model.DeviceType
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
        return
    }
    req.Vendor = strings.TrimSpace(req.Vendor)
    req.System = strings.TrimSpace(req.System)
    req.Kind = strings.TrimSpace(req.Kind)
    req.Tag = strings.TrimSpace(req.Tag)
    req.SSHType = strings.TrimSpace(req.SSHType)
    if req.Tag == "" {
        req.Tag = "default"
    }
    if req.Kind == "" {
        req.Kind = "all"
    }
    if req.Vendor == "" || req.System == "" || req.SSHType == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "vendor, system, ssh_type are required"})
        return
    }

    db := database.GetDB()
    var existing model.DeviceType
    if err := db.First(&existing, id).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
        return
    }
    existing.Vendor = req.Vendor
    existing.System = req.System
    existing.Kind = req.Kind
    existing.Tag = req.Tag
    existing.SSHType = req.SSHType
    // enabled 允许通过更新一起设置
    existing.Enabled = req.Enabled

    // 再次检查唯一组合冲突（排除自己）
    var count int64
    if err := db.Model(&model.DeviceType{}).
        Where("vendor = ? AND system = ? AND kind = ? AND tag = ? AND id <> ?", existing.Vendor, existing.System, existing.Kind, existing.Tag, existing.ID).
        Count(&count).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    if count > 0 {
        c.JSON(http.StatusConflict, gin.H{"error": "device type already exists"})
        return
    }

    if err := db.Save(&existing).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SetDeviceTypeEnabled POST /api/v1/device-types/:id/enabled
func SetDeviceTypeEnabled(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
        return
    }
    var body struct {
        Enabled bool `json:"enabled"`
    }
    if err := c.ShouldBindJSON(&body); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
        return
    }

    db := database.GetDB()
    var dt model.DeviceType
    if err := db.First(&dt, id).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
        return
    }
    dt.Enabled = body.Enabled
    if err := db.Save(&dt).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteDeviceType DELETE /api/v1/device-types/:id
func DeleteDeviceType(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
        return
    }
    if err := database.GetDB().Delete(&model.DeviceType{}, id).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"ok": true})
}