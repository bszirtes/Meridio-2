package bird

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBirdLog(t *testing.T) {
	tests := []struct {
		input   string
		want    BirdLog
		wantErr bool
	}{
		{
			input: "stderr:all",
			want:  BirdLog{Type: "stderr", Classes: []string{"all"}},
		},
		{
			input: "stderr:info,warning,error",
			want:  BirdLog{Type: "stderr", Classes: []string{"error", "info", "warning"}},
		},
		{
			input: "file:/var/log/bird.log:all",
			want:  BirdLog{Type: "file", Path: "/var/log/bird.log", Classes: []string{"all"}},
		},
		{
			input: "file:/var/log/bird.log:1048576:/var/log/bird.log.1:all",
			want:  BirdLog{Type: "file", Path: "/var/log/bird.log", Size: 1048576, BackupPath: "/var/log/bird.log.1", Classes: []string{"all"}},
		},
		{
			input: "fixed:/var/log/bird.log:1048576:all",
			want:  BirdLog{Type: "fixed", Path: "/var/log/bird.log", Size: 1048576, Classes: []string{"all"}},
		},
		{
			input: "syslog:bird:all",
			want:  BirdLog{Type: "syslog", Path: "bird", Classes: []string{"all"}},
		},
		{
			input: "udp:127.0.0.1:514:all",
			want:  BirdLog{Type: "udp", Path: "127.0.0.1", Port: 514, Classes: []string{"all"}},
		},
		// dedup classes
		{
			input: "stderr:info,info,warning",
			want:  BirdLog{Type: "stderr", Classes: []string{"info", "warning"}},
		},
		// errors
		{input: "stderr", wantErr: true},
		{input: "unknown:all", wantErr: true},
		{input: "file:/path:notanint:/backup:all", wantErr: true},
		{input: "file:/path:all:extra:fields:here", wantErr: true},
		{input: "fixed:/path:notanint:all", wantErr: true},
		{input: "udp:127.0.0.1:notaport:all", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseBirdLog(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFmtParams(t *testing.T) {
	tests := []struct {
		log  BirdLog
		want string
	}{
		{
			log:  BirdLog{Type: "stderr", Classes: []string{"all"}},
			want: `log stderr all;`,
		},
		{
			log:  BirdLog{Type: "stderr", Classes: []string{"error", "info"}},
			want: `log stderr { error, info };`,
		},
		{
			log:  BirdLog{Type: "file", Path: "/var/log/bird.log", Classes: []string{"all"}},
			want: `log "/var/log/bird.log" all;`,
		},
		{
			log:  BirdLog{Type: "file", Path: "/var/log/bird.log", Size: 1048576, BackupPath: "/var/log/bird.log.1", Classes: []string{"all"}},
			want: `log "/var/log/bird.log" 1048576 "/var/log/bird.log.1" all;`,
		},
		{
			log:  BirdLog{Type: "fixed", Path: "/var/log/bird.log", Size: 1048576, Classes: []string{"all"}},
			want: `log fixed "/var/log/bird.log" 1048576 all;`,
		},
		{
			log:  BirdLog{Type: "syslog", Path: "bird", Classes: []string{"all"}},
			want: `log syslog name "bird" all;`,
		},
		{
			log:  BirdLog{Type: "udp", Path: "127.0.0.1", Port: 514, Classes: []string{"all"}},
			want: `log udp 127.0.0.1 port 514 all;`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.log.FmtParams())
		})
	}
}
