package handles

import (
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
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
			want: "192.0.2.1",
		},
		{
			name: "missing user",
			ip:   "192.0.2.1",
			want: "192.0.2.1",
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
