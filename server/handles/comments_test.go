package handles

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeComment(t *testing.T) {
	tests := []struct {
		name     string
		input    createCommentReq
		wantBody string
		wantErr  bool
	}{
		{
			name:     "trim content",
			input:    createCommentReq{Content: "  hello  "},
			wantBody: "hello",
		},
		{
			name:     "accept unicode at content limit",
			input:    createCommentReq{Content: strings.Repeat("界", 400)},
			wantBody: strings.Repeat("界", 400),
		},
		{
			name:    "reject whitespace-only content",
			input:   createCommentReq{Content: " \n\t "},
			wantErr: true,
		},
		{
			name:    "reject content over limit",
			input:   createCommentReq{Content: strings.Repeat("界", 401)},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := normalizeComment(&test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeComment() error = %v, wantErr %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if test.input.Content != test.wantBody {
				t.Errorf("content length = %d, want %d", len([]rune(test.input.Content)), len([]rune(test.wantBody)))
			}
		})
	}
}

func TestCommentAuthor(t *testing.T) {
	tests := []struct {
		name string
		user *model.User
		ip   string
		want string
	}{
		{
			name: "logged-in user",
			user: &model.User{Username: "alice"},
			ip:   "192.0.2.1",
			want: "alice",
		},
		{
			name: "guest",
			user: &model.User{Role: model.GUEST},
			ip:   "192.0.2.1",
			want: "192. * . * .1",
		},
		{
			name: "missing user",
			ip:   "192.0.2.1",
			want: "192. * . * .1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := commentAuthor(test.user, test.ip); got != test.want {
				t.Errorf("commentAuthor() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCommentRateLimitKey(t *testing.T) {
	tests := []struct {
		name string
		user *model.User
		ip   string
		want string
	}{
		{name: "logged-in account", user: &model.User{Username: "alice"}, ip: "192.0.2.1", want: "user:alice"},
		{name: "guest IP", user: &model.User{Role: model.GUEST}, ip: "192.0.2.1", want: "guest:192.0.2.1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := commentRateLimitKey(test.user, test.ip); got != test.want {
				t.Errorf("commentRateLimitKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCommentCooldownDuration(t *testing.T) {
	tests := []struct {
		seconds int
		want    time.Duration
	}{
		{seconds: -1, want: 0},
		{seconds: 0, want: 0},
		{seconds: 30, want: 30 * time.Second},
		{seconds: maxCommentCooldownSeconds + 1, want: 24 * time.Hour},
	}
	for _, test := range tests {
		if got := commentCooldownDuration(test.seconds); got != test.want {
			t.Errorf("commentCooldownDuration(%d) = %v, want %v", test.seconds, got, test.want)
		}
	}
}

func TestMarkLegacyGuestComment(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "ipv4 guest", ip: "192.168.1.1", want: true},
		{name: "ipv6 guest", ip: "fe80:1:2:3::0", want: true},
		{name: "username", ip: "alice", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comment := model.Comment{Name: test.ip}
			markLegacyGuestComment(&comment)
			if comment.IsGuest != test.want {
				t.Errorf("IsGuest = %t, want %t", comment.IsGuest, test.want)
			}
		})
	}
}

func TestMaskCommentIP(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{
			name:    "ipv4",
			address: "127.0.0.1",
			want:    "127. * . * .1",
		},
		{
			name:    "compressed ipv6",
			address: "fe80:1:2:3::0",
			want:    "fe80: * : * :3::0",
		},
		{
			name:    "full ipv6",
			address: "2001:db8:1:2:3:4:5:6",
			want:    "2001: * : * :2:3:4:5:6",
		},
		{
			name:    "ipv4-mapped ipv6",
			address: "::ffff:192.168.1.1",
			want:    "::ffff:192. * . * .1",
		},
		{
			name:    "hexadecimal ipv4-mapped ipv6",
			address: "::ffff:c000:201",
			want:    "192. * . * .1",
		},
		{
			name:    "non-ip name",
			address: "alice",
			want:    "alice",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := maskCommentIP(test.address); got != test.want {
				t.Errorf("maskCommentIP(%q) = %q, want %q", test.address, got, test.want)
			}
		})
	}
}

func TestPrepareCommentForDisplay(t *testing.T) {
	tests := []struct {
		name         string
		comment      model.Comment
		showMaskedIP bool
		want         string
		wantGuest    bool
	}{
		{name: "user name", comment: model.Comment{Name: "alice", IP: "192.0.2.1"}, want: "alice"},
		{name: "user masked IP", comment: model.Comment{Name: "alice", IP: "192.0.2.1"}, showMaskedIP: true, want: "192. * . * .1"},
		{name: "guest name", comment: model.Comment{Name: "192. * . * .1", IP: "192.0.2.1", IsGuest: true}, want: "192. * . * .1", wantGuest: true},
		{name: "old user without IP", comment: model.Comment{Name: "alice"}, showMaskedIP: true, want: "alice"},
		{name: "old guest", comment: model.Comment{Name: "192.0.2.1", IsGuest: true}, want: "192. * . * .1", wantGuest: true},
		{name: "old guest without flag", comment: model.Comment{Name: "192.0.2.1"}, showMaskedIP: true, want: "192. * . * .1", wantGuest: true},
		{name: "IP-shaped user name", comment: model.Comment{Name: "192.0.2.1", IP: "198.51.100.2"}, want: "192.0.2.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareCommentForDisplay(&tt.comment, tt.showMaskedIP)
			if tt.comment.Name != tt.want || tt.comment.IsGuest != tt.wantGuest {
				t.Errorf("got name %q and guest %t, want %q and %t", tt.comment.Name, tt.comment.IsGuest, tt.want, tt.wantGuest)
			}
		})
	}
}

func TestCommentHandlersStoreIPAndApplyDisplaySetting(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conf.Conf = conf.DefaultConfig(t.TempDir())
	db.Init(database)
	op.SettingCacheUpdate()
	t.Cleanup(op.SettingCacheUpdate)
	settings := []model.SettingItem{
		{Key: conf.CommentsEnabled, Value: "true"},
		{Key: conf.CommentsCooldown, Value: "0"},
		{Key: conf.CommentsShowMaskedIP, Value: "false"},
	}
	if err := db.SaveSettingItems(settings); err != nil {
		t.Fatal(err)
	}

	post := func(user *model.User, ip string) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		req := httptest.NewRequest(http.MethodPost, "/public/comments", strings.NewReader(`{"content":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = net.JoinHostPort(ip, "1234")
		ctx.Request = req.WithContext(context.WithValue(req.Context(), conf.UserKey, user))
		CreateComment(ctx)
		return recorder.Body.String()
	}
	for _, tt := range []struct {
		user *model.User
		ip   string
		name string
	}{
		{user: &model.User{Role: model.GUEST}, ip: "192.0.2.1", name: "192. * . * .1"},
		{user: &model.User{Role: model.GUEST}, ip: "2001:db8:1:2:3:4:5:6", name: "2001: * : * :2:3:4:5:6"},
		{user: &model.User{Username: "alice"}, ip: "198.51.100.2", name: "alice"},
	} {
		body := post(tt.user, tt.ip)
		var response struct {
			Code int `json:"code"`
			Data struct {
				Name string  `json:"name"`
				IP   *string `json:"ip"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || response.Data.Name != tt.name || response.Data.IP != nil || strings.Contains(body, tt.ip) {
			t.Fatalf("unexpected create response: %s", body)
		}
		var saved model.Comment
		if err := database.Order("id DESC").First(&saved).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Name != tt.name || saved.IP != tt.ip {
			t.Fatalf("stored name/IP = %q/%q, want %q/%q", saved.Name, saved.IP, tt.name, tt.ip)
		}
	}
	if err := database.Create(&model.Comment{Name: "192.0.2.9", Content: "legacy"}).Error; err != nil {
		t.Fatal(err)
	}
	list := func() string {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/public/comments", nil)
		ListComments(ctx)
		return recorder.Body.String()
	}
	body := list()
	if !strings.Contains(body, `"name":"alice"`) || !strings.Contains(body, `"name":"192. * . * .9"`) || strings.Contains(body, "192.0.2.9") {
		t.Fatalf("unexpected list response with names: %s", body)
	}
	if err := database.Model(&model.SettingItem{}).Where("key = ?", conf.CommentsShowMaskedIP).Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	op.SettingCacheUpdate()
	body = post(&model.User{Username: "bob"}, "203.0.113.3")
	if !strings.Contains(body, `"name":"203. * . * .3"`) || strings.Contains(body, "203.0.113.3") {
		t.Fatalf("unexpected create response with masked IP: %s", body)
	}
	body = list()
	if !strings.Contains(body, `"name":"198. * . * .2"`) || strings.Contains(body, "198.51.100.2") || strings.Contains(body, `"ip":`) {
		t.Fatalf("unexpected list response with masked IP: %s", body)
	}
	var bob model.Comment
	if err := database.Where("name = ?", "bob").First(&bob).Error; err != nil {
		t.Fatal(err)
	}
	if bob.IP != "203.0.113.3" {
		t.Fatalf("stored IP = %q", bob.IP)
	}
	if err := database.Model(&model.SettingItem{}).Where("key = ?", conf.CommentsShowMaskedIP).Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	op.SettingCacheUpdate()
	if body = list(); !strings.Contains(body, `"name":"bob"`) {
		t.Fatalf("switching back did not restore the stored name: %s", body)
	}
	if body = post(&model.User{Role: model.GUEST}, "invalid"); !strings.Contains(body, `"code":400`) {
		t.Fatalf("invalid IP was accepted: %s", body)
	}
}

func TestMaskGuestCommentName(t *testing.T) {
	tests := []struct {
		name    string
		comment model.Comment
		want    string
	}{
		{
			name:    "mask guest",
			comment: model.Comment{Name: "127.0.0.1", IsGuest: true},
			want:    "127. * . * .1",
		},
		{
			name:    "keep user name",
			comment: model.Comment{Name: "alice", IsGuest: false},
			want:    "alice",
		},
		{
			name:    "keep ip-shaped authenticated username",
			comment: model.Comment{Name: "127.0.0.1", IsGuest: false},
			want:    "127.0.0.1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comment := test.comment
			maskGuestCommentName(&comment)
			if comment.Name != test.want {
				t.Errorf("Name = %q, want %q", comment.Name, test.want)
			}
		})
	}
}

func TestMaskLegacyGuestCommentName(t *testing.T) {
	comment := model.Comment{Name: "fe80:1:2:3::0"}
	markLegacyGuestComment(&comment)
	maskGuestCommentName(&comment)

	if !comment.IsGuest {
		t.Fatal("legacy IP comment was not marked as a guest")
	}
	if want := "fe80: * : * :3::0"; comment.Name != want {
		t.Errorf("Name = %q, want %q", comment.Name, want)
	}
}
