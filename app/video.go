package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// videoCmd video 命令组
func videoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "video",
		Short: "视频处理（组装、转场）",
		Long: `视频处理命令组

支持的操作：
  assemble  - 将多张图片通过 xfade 转场效果组装成视频`,
	}

	cmd.AddCommand(videoAssembleCmd())

	return cmd
}

// xfadeEffects 可用的 xfade 转场效果池
var xfadeEffects = []string{
	"fade", "wipeleft", "wiperight", "slideup", "slidedown",
	"pixelize", "circlecrop", "hlslice", "radial", "smoothleft",
}

// videoAssembleCmd 将多张图片组装成视频
func videoAssembleCmd() *cobra.Command {
	var duration int
	var transition int
	var effects string
	var resolution string
	var output string

	cmd := &cobra.Command{
		Use:   "assemble <images_or_dir>...",
		Short: "将多张图片通过 xfade 转场效果组装成视频",
		Long: `将多张图片通过 ffmpeg xfade 转场效果组装成视频

参数可以是：
  - 多个图片文件路径（按给定顺序）
  - 一个目录路径（扫描 *.png, *.jpg, *.jpeg，按文件名排序）

需要安装 ffmpeg（brew install ffmpeg）

转场效果池：
  fade, wipeleft, wiperight, slideup, slidedown, pixelize, circlecrop, hlslice, radial, smoothleft

示例：
  # 用三张图片组装视频（随机转场）
  anbanwriter video assemble img1.png img2.png img3.png

  # 用目录中的图片组装视频
  anbanwriter video assemble output/xls/xls-20240101-001/

  # 指定输出文件和转场效果
  anbanwriter video assemble img1.png img2.png -o result.mp4 --effects "fade,wipeleft"

  # 指定每张图片时长和转场时长
  anbanwriter video assemble dir/ --duration 5 --transition 2

  # 指定分辨率
  anbanwriter video assemble dir/ --resolution 1080x1440`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 解析输入图片列表
			images, err := resolveImageInputs(args)
			if err != nil {
				responseError(err)
				return nil
			}

			// 2. 验证：至少 2 张
			if len(images) < 2 {
				responseError(fmt.Errorf("至少需要 2 张图片，当前只有 %d 张", len(images)))
				return nil
			}

			// 3. 检查 ffmpeg 可用性
			ffmpegPath, err := exec.LookPath("ffmpeg")
			if err != nil {
				responseError(fmt.Errorf("ffmpeg 未找到，请先安装：brew install ffmpeg"))
				return nil
			}

			// 4. 确定分辨率
			w, h, err := resolveResolution(resolution, images[0])
			if err != nil {
				responseError(fmt.Errorf("获取分辨率失败: %w", err))
				return nil
			}

			// 5. 确定输出路径
			outPath := output
			if outPath == "" {
				// 输出到第一张图片所在目录
				outPath = filepath.Join(filepath.Dir(images[0]), "video.mp4")
			}

			// 6. 解析转场效果列表
			effectList := parseEffects(effects)

			// 7. 构建并执行 ffmpeg 命令
			videoPath, err := runFFmpegAssemble(ffmpegPath, images, w, h, duration, transition, effectList, outPath)
			if err != nil {
				responseError(err)
				return nil
			}

			totalDuration := len(images)*duration - (len(images)-1)*(duration-transition)
			if totalDuration < 0 {
				totalDuration = len(images) * duration
			}

			responseSuccess(map[string]any{
				"video_path":    videoPath,
				"image_count":   len(images),
				"resolution":    fmt.Sprintf("%dx%d", w, h),
				"duration_secs": totalDuration,
				"images":        images,
			})
			return nil
		},
	}

	cmd.Flags().IntVar(&duration, "duration", 3, "每张图片显示秒数")
	cmd.Flags().IntVar(&transition, "transition", 1, "转场时长（秒）")
	cmd.Flags().StringVar(&effects, "effects", "", "转场效果列表（逗号分隔），为空则随机选择。可选值: fade,wipeleft,wiperight,slideup,slidedown,pixelize,circlecrop,hlslice,radial,smoothleft")
	cmd.Flags().StringVar(&resolution, "resolution", "", "视频分辨率 WxH（如 1080x1440），为空则自动检测首张图片尺寸")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出视频路径（默认为图片所在目录的 video.mp4）")

	return cmd
}

// resolveImageInputs 解析位置参数为有序图片路径列表
func resolveImageInputs(args []string) ([]string, error) {
	// 如果只有一个参数且是目录，扫描目录
	if len(args) == 1 {
		info, err := os.Stat(args[0])
		if err != nil {
			return nil, fmt.Errorf("路径不存在: %s", args[0])
		}
		if info.IsDir() {
			return scanImageDir(args[0])
		}
	}

	// 多个参数或单个文件：按给定顺序，验证存在性
	var images []string
	for _, p := range args {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("文件不存在: %s", p)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("混合模式不支持目录，请单独传入目录路径: %s", p)
		}
		images = append(images, p)
	}
	return images, nil
}

// scanImageDir 扫描目录中的图片文件（按文件名排序）
func scanImageDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	var images []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") {
			images = append(images, filepath.Join(dir, e.Name()))
		}
	}

	sort.Strings(images)

	if len(images) == 0 {
		return nil, fmt.Errorf("目录中没有找到图片文件（*.png, *.jpg, *.jpeg）: %s", dir)
	}

	return images, nil
}

// resolveResolution 确定视频分辨率
func resolveResolution(res string, firstImage string) (int, int, error) {
	if res != "" {
		return parseResolution(res)
	}

	// 尝试用 ffprobe 检测首张图片尺寸
	if _, err := exec.LookPath("ffprobe"); err == nil {
		w, h, probeErr := probeImageSize(firstImage)
		if probeErr == nil {
			return w, h, nil
		}
	}

	// 默认分辨率
	return 1080, 1440, nil
}

// parseResolution 解析 WxH 格式
func parseResolution(res string) (int, int, error) {
	res = strings.ToLower(res)
	sep := "x"
	parts := strings.SplitN(res, sep, 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("分辨率格式无效，应为 WxH（如 1080x1440）: %s", res)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("宽度无效: %s", parts[0])
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("高度无效: %s", parts[1])
	}
	return w, h, nil
}

// probeImageSize 用 ffprobe 获取图片尺寸
func probeImageSize(imgPath string) (int, int, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=s=x:p=0",
		imgPath,
	).Output()
	if err != nil {
		return 0, 0, err
	}
	line := strings.TrimSpace(string(out))
	parts := strings.SplitN(line, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("ffprobe 输出格式异常: %s", line)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

// parseEffects 解析转场效果列表
func parseEffects(effects string) []string {
	if effects == "" {
		return nil
	}
	var result []string
	for e := range strings.SplitSeq(effects, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			result = append(result, e)
		}
	}
	return result
}

// pickEffect 从效果列表（或全局池）中随机选一个
func pickEffect(effectList []string) string {
	pool := effectList
	if len(pool) == 0 {
		pool = xfadeEffects
	}
	return pool[rand.Intn(len(pool))]
}

// runFFmpegAssemble 构建 ffmpeg filter_complex 并执行
func runFFmpegAssemble(ffmpegPath string, images []string, w, h, duration, transition int, effectList []string, outPath string) (string, error) {
	n := len(images)

	// 构建参数：每张图片 -loop 1 -t {duration} -i {img}
	var cmdArgs []string
	for _, img := range images {
		cmdArgs = append(cmdArgs, "-loop", "1", "-t", strconv.Itoa(duration), "-i", img)
	}

	// 构建 filter_complex
	// 1. scale 每个输入流
	// 2. xfade 链式拼接
	var filterParts []string

	// scale filters：[0:v]scale=W:H:...[v0], [1:v]scale=...  [v1], ...
	scaleFmt := "scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1"
	for i := range n {
		scaleFilter := fmt.Sprintf("[%d:v]"+scaleFmt+"[v%d]", i, w, h, w, h, i)
		filterParts = append(filterParts, scaleFilter)
	}

	// xfade filters：链式 [v0][v1]xfade=effect=...:duration=...:offset=...[xf1]; [xf1][v2]xfade=...[xf2]; ...
	// offset = (duration - transition) * i  (每段的结束时间点)
	prevLabel := "v0"
	for i := 1; i < n; i++ {
		effect := pickEffect(effectList)
		offset := (duration - transition) * i
		outLabel := fmt.Sprintf("xf%d", i)
		xfadeFilter := fmt.Sprintf("[%s][v%d]xfade=transition=%s:duration=%d:offset=%d[%s]",
			prevLabel, i, effect, transition, offset, outLabel)
		filterParts = append(filterParts, xfadeFilter)
		prevLabel = outLabel
	}

	filterComplex := strings.Join(filterParts, "; ")
	finalLabel := prevLabel // 最后一个 xfN 或 v0（只有 1 张时不会到这里）

	cmdArgs = append(cmdArgs,
		"-filter_complex", filterComplex,
		"-map", fmt.Sprintf("[%s]", finalLabel),
		"-pix_fmt", "yuv420p",
		"-y",
		outPath,
	)

	// 确保输出目录存在
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return "", fmt.Errorf("创建输出目录失败: %w", err)
	}

	// 执行 ffmpeg
	execCmd := exec.Command(ffmpegPath, cmdArgs...)
	out, err := execCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg 执行失败: %w\n%s", err, string(out))
	}

	return outPath, nil
}
