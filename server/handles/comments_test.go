package handles

import (
	"strings"
	"testing"

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
