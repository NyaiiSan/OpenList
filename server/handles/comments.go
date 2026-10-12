package handles

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

const (
	defaultCommentLimit  = 20
	maxCommentLimit      = 100
	maxCommentNameLength = 255
	commentCooldown      = 30 * time.Second
)

var commentPosts = struct {
	sync.Mutex
	last map[string]time.Time
}{last: make(map[string]time.Time)}

type commentPageReq struct {
	Offset int `form:"offset" binding:"gte=0"`
	Limit  int `form:"limit" binding:"gte=0"`
}

type createCommentReq struct {
	Content string `json:"content" binding:"required"`
}

func normalizeComment(req *createCommentReq) error {
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" || len([]rune(req.Content)) > 400 {
		return errors.New("comment content must contain between 1 and 400 characters")
	}
	return nil
}

func commentAuthor(user *model.User, ip string) string {
	if user == nil || user.IsGuest() {
		return ip
	}
	return user.Username
}

func markLegacyGuestComment(comment *model.Comment) {
	if !comment.IsGuest && net.ParseIP(comment.Name) != nil {
		comment.IsGuest = true
	}
}

func ListComments(c *gin.Context) {
	if !setting.GetBool(conf.CommentsEnabled) {
		common.ErrorStrResp(c, "Comments are disabled", 404)
		return
	}
	var req commentPageReq
	if err := c.ShouldBindQuery(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if req.Limit == 0 {
		req.Limit = defaultCommentLimit
	}
	if req.Limit > maxCommentLimit {
		req.Limit = maxCommentLimit
	}
	var comments []model.Comment
	query := db.GetDb().Model(&model.Comment{})
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if err := query.Order("created_at DESC").Order("id DESC").Offset(req.Offset).Limit(req.Limit).Find(&comments).Error; err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	for i := range comments {
		markLegacyGuestComment(&comments[i])
	}
	common.SuccessResp(c, common.PageResp{Content: comments, Total: total})
}

func CreateComment(c *gin.Context) {
	if !setting.GetBool(conf.CommentsEnabled) {
		common.ErrorStrResp(c, "Comments are disabled", 404)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var req createCommentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if err := normalizeComment(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	user, ok := c.Request.Context().Value(conf.UserKey).(*model.User)
	if !ok || user == nil {
		common.ErrorStrResp(c, "Could not determine comment author", 500)
		return
	}
	name := commentAuthor(user, c.ClientIP())
	if len([]rune(name)) > maxCommentNameLength {
		common.ErrorStrResp(c, "Comment author name exceeds the maximum length", 400)
		return
	}

	commentPosts.Lock()
	lastPost, posted := commentPosts.last[c.ClientIP()]
	now := time.Now()
	if posted && now.Sub(lastPost) < commentCooldown {
		commentPosts.Unlock()
		common.ErrorStrResp(c, "Please wait before posting another comment", 429)
		return
	}
	commentPosts.last[c.ClientIP()] = now
	if len(commentPosts.last) > 1000 {
		for ip, timestamp := range commentPosts.last {
			if now.Sub(timestamp) >= commentCooldown {
				delete(commentPosts.last, ip)
			}
		}
	}
	commentPosts.Unlock()

	comment := model.Comment{
		Name:    name,
		IsGuest: user.IsGuest(),
		Content: req.Content,
	}
	if err := db.GetDb().Create(&comment).Error; err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, comment)
}
