package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// 与 Python 版 _generate_peaks 对拍：同一份音频、同一个时长，两边的桶数与每桶数值必须逐个相等。
// 波形不一致的后果是同一个视频换条导入路径就换一副波形，用户会以为轴对不上。
func TestPeaksMatchesPythonReference(t *testing.T) {
	sample := os.Getenv("PEAKS_SAMPLE")
	reference := os.Getenv("PEAKS_REFERENCE")
	if sample == "" || reference == "" {
		t.Skip("需要 PEAKS_SAMPLE / PEAKS_REFERENCE 环境变量")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("没有系统 ffmpeg")
	}
	manager := newFFmpegManagerWithOptions(ffmpegOptions{Path: ffmpeg})
	manager.status.State = "ready"
	engine := newMediaEngine(manager, t.TempDir(), nil)

	duration, err := strconv.ParseFloat(os.Getenv("PEAKS_DUR"), 64)
	if err != nil {
		t.Fatalf("PEAKS_DUR 无效: %v", err)
	}
	got, err := engine.Peaks(context.Background(), sample, duration)
	if err != nil {
		t.Fatalf("Peaks 失败: %v", err)
	}
	raw, err := os.ReadFile(reference)
	if err != nil {
		t.Fatalf("读参考结果失败: %v", err)
	}
	var want PeaksResult
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("解析参考结果失败: %v", err)
	}
	if len(got.Peaks) != len(want.Peaks) {
		t.Fatalf("桶数不一致: go=%d python=%d", len(got.Peaks), len(want.Peaks))
	}
	if got.PerSec != want.PerSec {
		t.Fatalf("per_sec 不一致: go=%v python=%v", got.PerSec, want.PerSec)
	}
	for i := range got.Peaks {
		if got.Peaks[i] != want.Peaks[i] {
			t.Fatalf("第 %d 桶不一致: go=%v python=%v", i, got.Peaks[i], want.Peaks[i])
		}
	}
}
