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
	defaultCommentLimit           = 20
	maxCommentLimit               = 100
	maxCommentNameLength          = 255
	defaultCommentCooldownSeconds = 30
	maxCommentCooldownSeconds     = 86400
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

type deleteCommentReq struct {
	ID uint `uri:"id" binding:"required,min=1"`
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

func commentRateLimitKey(user *model.User, ip string) string {
	if user == nil || user.IsGuest() {
		return "guest:" + ip
	}
	return "user:" + user.Username
}

func commentCooldownDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	if seconds > maxCommentCooldownSeconds {
		seconds = maxCommentCooldownSeconds
	}
	duration := time.Duration(seconds)
	maxDuration := time.Duration(1<<63 - 1)
	if duration > maxDuration/time.Second {
		return maxDuration
	}
	return duration * time.Second
}

func markLegacyGuestComment(comment *model.Comment) {
	if !comment.IsGuest && net.ParseIP(comment.Name) != nil {
		comment.IsGuest = true
	}
}

func maskGuestCommentName(comment *model.Comment) {
	if comment.IsGuest {
		comment.Name = maskCommentIP(comment.Name)
	}
}

func maskCommentIP(address string) string {
	ip := net.ParseIP(address)
	if ip == nil {
		return address
	}
	if ip.To4() != nil {
		if colon := strings.LastIndex(address, ":"); colon >= 0 {
			if masked, ok := maskIPv4CommentIP(address[colon+1:]); ok {
				return address[:colon+1] + masked
			}
		}
		if masked, ok := maskIPv4CommentIP(address); ok {
			return masked
		}
		return address
	}

	halves := strings.Split(address, "::")
	if len(halves) > 2 {
		return address
	}
	left := []string{}
	if halves[0] != "" {
		left = strings.Split(halves[0], ":")
	}
	right := []string{}
	if len(halves) == 2 && halves[1] != "" {
		right = strings.Split(halves[1], ":")
	}
	omittedGroups := 8 - len(left) - len(right)
	if (len(halves) == 1 && omittedGroups != 0) ||
		(len(halves) == 2 && omittedGroups < 1) {
		return address
	}

	groups := append([]string{}, left...)
	for i := 0; i < omittedGroups; i++ {
		groups = append(groups, "0")
	}
	groups = append(groups, right...)
	if len(groups) != 8 {
		return address
	}
	groups[1], groups[2] = " * ", " * "

	if omittedGroups > 0 {
		omittedStart := len(left)
		omittedEnd := omittedStart + omittedGroups - 1
		if omittedStart <= 2 && omittedEnd >= 1 {
			return strings.Join(groups, ":")
		}
		maskedLeft := maskIPv6Groups(left, 0)
		maskedRight := maskIPv6Groups(right, len(left)+omittedGroups)
		return strings.Join(maskedLeft, ":") + "::" + strings.Join(maskedRight, ":")
	}
	return strings.Join(groups, ":")
}

func maskIPv4CommentIP(address string) (string, bool) {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() == nil || strings.Contains(address, ":") {
		return address, false
	}
	octets := strings.Split(address, ".")
	if len(octets) != 4 {
		return address, false
	}
	return octets[0] + ". * . * ." + octets[3], true
}

func maskIPv6Groups(groups []string, start int) []string {
	masked := append([]string{}, groups...)
	for i := range masked {
		if start+i == 1 || start+i == 2 {
			masked[i] = " * "
		}
	}
	return masked
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
		maskGuestCommentName(&comments[i])
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

	cooldownSeconds := setting.GetInt(conf.CommentsCooldown, defaultCommentCooldownSeconds)
	cooldown := commentCooldownDuration(cooldownSeconds)
	rateLimitKey := commentRateLimitKey(user, c.ClientIP())
	now := time.Now()
	if cooldown > 0 {
		commentPosts.Lock()
		lastPost, posted := commentPosts.last[rateLimitKey]
		if posted && now.Sub(lastPost) < cooldown {
			commentPosts.Unlock()
			common.ErrorStrResp(c, "Please wait before posting another comment", 429)
			return
		}
		commentPosts.last[rateLimitKey] = now
		if len(commentPosts.last) > 1000 {
			for key, timestamp := range commentPosts.last {
				if now.Sub(timestamp) >= cooldown {
					delete(commentPosts.last, key)
				}
			}
		}
		commentPosts.Unlock()
	}

	comment := model.Comment{
		Name:    name,
		IsGuest: user.IsGuest(),
		Content: req.Content,
	}
	if err := db.GetDb().Create(&comment).Error; err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	maskGuestCommentName(&comment)
	common.SuccessResp(c, comment)
}

func DeleteComment(c *gin.Context) {
	var req deleteCommentReq
	if err := c.ShouldBindUri(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	result := db.GetDb().Delete(&model.Comment{}, req.ID)
	if result.Error != nil {
		common.ErrorResp(c, result.Error, 500)
		return
	}
	if result.RowsAffected == 0 {
		common.ErrorStrResp(c, "Comment not found", 404)
		return
	}
	common.SuccessResp(c)
}
