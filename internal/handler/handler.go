package handler

import (
	"errors"
	"live-stream/internal/auth"
	"live-stream/internal/middleware"
	"live-stream/internal/model"
	"live-stream/internal/service"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func RegisterHandler(c *gin.Context) {
	var req model.RegisterRequest
	err := c.ShouldBind(&req)
	if err != nil {
		log.Printf("capture register params error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "请求参数错误",
		})
		return
	}
	user, err := service.UserReigister(c.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidEamil) {
			c.JSON(http.StatusConflict, gin.H{
				"message": "邮箱已被注册",
			})
			log.Printf("register failed: %v", err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    user,
		"message": "注册成功",
	})
}

func LoginHandler(c *gin.Context) {
	var req model.LoginRequest
	err := c.ShouldBind(&req)
	if err != nil {
		log.Printf("capture login params error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "请求参数错误",
		})
		return
	}
	user, tokenStr, err := service.UserLogin(c.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "邮箱或密码错误",
			})
			return
		}
		log.Printf("login failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "服务器内部错误",
		})
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"sid",
		tokenStr,
		7*24*3600,
		"/",
		"",
		true,
		true,
	)

	c.JSON(http.StatusOK, gin.H{
		"data":    user,
		"message": "登录成功",
	})
}

func LogOut(c *gin.Context) {
	sid, err := c.Cookie("sid")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "未登录",
		})
		return
	}

	if err := auth.DelSession(c.Request.Context(), sid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "登出失败",
		})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("sid", "", -1, "/", "", true, true)
	c.JSON(http.StatusOK, gin.H{
		"message": "登出成功",
	})
}

func GetMe(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "未登录",
		})
		return
	}
	user, err := service.GetUserByID(c, uid)
	if err != nil {
		log.Printf("getme failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "查询用户失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    user,
		"message": "查询用户成功",
	})

}

func CreateRoom(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
		return
	}
	var req model.CreateRoomRequest
	err := c.ShouldBind(req)
	if err != nil {
		log.Printf("capture create room params failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "请求参数错误",
		})
		return
	}
	room, err := service.CreateRoom(c.Request.Context(), uid, &req)
	if err != nil {
		if err == service.ErrInvalidOwnerID {
			c.JSON(http.StatusConflict, gin.H{
				"message": "用户已经拥有房间",
			})
			return
		}
		log.Printf("create room failed: %v", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    room,
		"message": "房间创建成功",
	})

}

func ListRoom(c *gin.Context) {
	rooms, err := service.ListRooms(c)
	if err != nil {
		log.Printf("list rooms failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "房间列表返回失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    rooms,
		"message": "房间列表返回成功",
	})
}

func GetRoomDetail(c *gin.Context) {
	// 从url路径取字符串
	roomIDStr := c.Param("id")
	// 转uint64（和repo里roomID类型保持一致）
	roomID, err := strconv.ParseUint(roomIDStr, 10, 64)
	detail, err := service.RoomDetail(c, roomID)
	if err != nil {
		log.Printf("detail room failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "房间信息返回失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    detail,
		"message": "房间信息返回成功",
	})
}

func UpdateRoom(c *gin.Context) {
	roomID, err := strconv.ParseUint(c.Param("room_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "未能获取房间",
		})
		return
	}
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "未登录",
		})
		return
	}
	var updateRoomRequest model.UpdateRoomRequest
	c.ShouldBind(updateRoomRequest)
	room, err := service.UpdateRoom(c, &updateRoomRequest, uid, roomID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRoomID) {
			log.Printf("update room failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "房间不存在",
			})
			return
		} else if errors.Is(err, service.ErrNotRoomOwner) {
			log.Printf("update room failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "仅房主可修改房间标题",
			})
			return
		}
		log.Printf("update room failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "房间标题修改失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    room,
		"message": "房间标题修改成功",
	})

}

func StartRoom(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "未登录",
		})
		return
	}

	// 路径参数 room_id
	roomIDStr := c.Param("id")
	roomID, err := strconv.ParseUint(roomIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "room id格式错误",
		})
		return
	}
	// 接收body里的room_title
	var req model.StartRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "参数错误，room_title不能为空",
		})
		return
	}
	// 调用service，service只返回error
	err = service.StartLiveService(c.Request.Context(), roomID, req.RoomTitle, uid)
	if err != nil {
		log.Printf("start room failed: %v", err)
		if errors.Is(err, service.ErrInvalidRoomID) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "房间不存在"})
		} else if errors.Is(err, service.ErrNotRoomOwner) {
			c.JSON(http.StatusForbidden, gin.H{"message": "仅房主可开播"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "开播失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "开播成功",
	})
}

func StopRoom(c *gin.Context) {
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "未登录",
		})
		return
	}
	// 路径参数 room_id
	roomIDStr := c.Param("id")
	roomID, err := strconv.ParseUint(roomIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "room id格式错误",
		})
		return
	}
	err = service.StopLiveService(c.Request.Context(), roomID, uid)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRoomID) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "房间不存在"})
		} else if errors.Is(err, service.ErrNotRoomOwner) {
			c.JSON(http.StatusForbidden, gin.H{"message": "仅房主可开播"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "开播失败"})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "关播成功",
	})
}

func GetDanmaHistory(c *gin.Context) {
	roomIDStr := c.Param("room_id")
	roomID, err := strconv.ParseUint(roomIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "room id格式错误",
		})
		return
	}
	beforeID, err := strconv.ParseUint(c.DefaultQuery("before_id", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "cursor格式错误",
		})
		return
	}
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "limit格式错误",
		})
		return
	}

	data, err := service.GetDanmaHistory(c, roomID, beforeID, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "读取历史弹幕失败",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    data,
		"message": "读取历史弹幕成功",
	})

}
