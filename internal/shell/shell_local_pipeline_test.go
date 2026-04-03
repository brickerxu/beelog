package shell

import (
	"testing"
)

func TestParseLocalPipeline(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantRemote string
		wantLocal  string
		wantHas    bool
	}{
		{
			name:       "不含 |>",
			input:      "tail -100 /var/log/app.log",
			wantRemote: "tail -100 /var/log/app.log",
			wantLocal:  "",
			wantHas:    false,
		},
		{
			name:       "基本 |> 用法",
			input:      "tail -100 /var/log/app.log |> grep ERROR",
			wantRemote: "tail -100 /var/log/app.log",
			wantLocal:  "grep ERROR",
			wantHas:    true,
		},
		{
			name:       "本地多管道",
			input:      "ps aux |> grep java | sort | uniq -c",
			wantRemote: "ps aux",
			wantLocal:  "grep java | sort | uniq -c",
			wantHas:    true,
		},
		{
			name:       "|> 两侧有多余空格",
			input:      "echo hello  |>  cat",
			wantRemote: "echo hello",
			wantLocal:  "cat",
			wantHas:    true,
		},
		{
			name:       "远程命令为空",
			input:      "|> grep ERROR",
			wantRemote: "|> grep ERROR",
			wantLocal:  "",
			wantHas:    false,
		},
		{
			name:       "本地命令为空",
			input:      "echo hello |>",
			wantRemote: "echo hello |>",
			wantLocal:  "",
			wantHas:    false,
		},
		{
			name:       "只识别第一个 |>",
			input:      "cmd |> awk '{print $1}' |> sort",
			wantRemote: "cmd",
			wantLocal:  "awk '{print $1}' |> sort",
			wantHas:    true,
		},
		{
			name:       "空字符串",
			input:      "",
			wantRemote: "",
			wantLocal:  "",
			wantHas:    false,
		},
		{
			name:       "仅 |>",
			input:      "|>",
			wantRemote: "|>",
			wantLocal:  "",
			wantHas:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRemote, gotLocal, gotHas := parseLocalPipeline(tt.input)
			if gotRemote != tt.wantRemote {
				t.Errorf("remoteCmd: got %q, want %q", gotRemote, tt.wantRemote)
			}
			if gotLocal != tt.wantLocal {
				t.Errorf("localCmd: got %q, want %q", gotLocal, tt.wantLocal)
			}
			if gotHas != tt.wantHas {
				t.Errorf("hasLocal: got %v, want %v", gotHas, tt.wantHas)
			}
		})
	}
}
