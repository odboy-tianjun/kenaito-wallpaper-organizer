package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// supportedExts 支持的图片格式
var supportedExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".bmp": true, ".webp": true, ".gif": true,
	".tiff": true, ".tif": true,
}

// WallpaperInfo 存储壁纸信息
type WallpaperInfo struct {
	Path       string
	DarkScore  float64 // 黑暗程度评分（0-1，越低越暗）
	LightScore float64 // 亮度程度评分（0-1，越高越亮）
	AvgColor   color.RGBA
	Size       int64
	Width      int
	Height     int
}

// Config 配置结构
type Config struct {
	SourceDir    string // 源目录
	TargetDir    string // 目标目录基名（自动按黑暗等级创建子目录）
	MinWidth     int    // 图片最小宽度
	MinHeight    int    // 图片最小高度
	MinSizeKB    int64  // 文件大小最小值（KB）
	MaxSizeMB    int64  // 文件大小最大值（MB）
	CopyMode     bool   // true=复制, false=移动
	Parallel     int    // 并发数
	Verbose      bool   // 是否打印详细日志
	SleepSeconds int    // 执行前等待秒数（0=不等待）
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		SourceDir:    ".",
		MinWidth:     1080,
		MinHeight:    720,
		MinSizeKB:    100,
		MaxSizeMB:    100,
		CopyMode:     false,
		Parallel:     24,
		Verbose:      true,
		SleepSeconds: 5,
	}
}

func main() {
	config := DefaultConfig()

	// 命令行参数
	flag.StringVar(&config.SourceDir, "source", config.SourceDir, "源目录路径")
	flag.StringVar(&config.TargetDir, "target", "", "目标目录基名（留空则自动用源目录名）")
	flag.IntVar(&config.MinWidth, "min-width", config.MinWidth, "图片最小宽度（像素）")
	flag.IntVar(&config.MinHeight, "min-height", config.MinHeight, "图片最小高度（像素）")
	flag.Int64Var(&config.MinSizeKB, "min-size", config.MinSizeKB, "最小文件大小（KB）")
	flag.Int64Var(&config.MaxSizeMB, "max-size", config.MaxSizeMB, "最大文件大小（MB）")
	flag.BoolVar(&config.CopyMode, "copy", config.CopyMode, "复制模式（默认移动）")
	flag.IntVar(&config.Parallel, "parallel", config.Parallel, "并发数")
	flag.BoolVar(&config.Verbose, "verbose", config.Verbose, "详细输出")
	flag.IntVar(&config.SleepSeconds, "sleep", config.SleepSeconds, "执行前等待秒数（0=不等待）")
	flag.Parse()

	// 检查源目录
	if _, err := os.Stat(config.SourceDir); os.IsNotExist(err) {
		log.Fatalf("源目录不存在: %s", config.SourceDir)
	}

	absSource, _ := filepath.Abs(config.SourceDir)
	baseDirName := filepath.Base(absSource)

	// 如果目标目录为空，自动生成
	if config.TargetDir == "" {
		config.TargetDir = filepath.Join(absSource, baseDirName+"_dark_wallpapers")
	} else {
		if !filepath.IsAbs(config.TargetDir) {
			config.TargetDir = filepath.Join(absSource, config.TargetDir)
		}
	}

	fmt.Println("🖼️  Kenaito Wallpaper Organizer 壁纸整理工具")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("  源目录:     %s\n", config.SourceDir)
	fmt.Printf("  目标目录:   %s\n", config.TargetDir)
	fmt.Printf("  最小分辨率: %dx%d\n", config.MinWidth, config.MinHeight)
	fmt.Printf("  文件大小:   %dKB ~ %dMB\n", config.MinSizeKB, config.MaxSizeMB)
	fmt.Printf("  模式:       %s\n", map[bool]string{true: "复制", false: "移动"}[config.CopyMode])
	fmt.Println(strings.Repeat("=", 50))

	if config.SleepSeconds > 0 {
		fmt.Printf("\n⏳ 程序将在 %d 秒后开始执行（Ctrl+C 取消）...\n", config.SleepSeconds)
		time.Sleep(time.Duration(config.SleepSeconds) * time.Second)
	}

	// 黑暗阈值等级（从高到低）
	thresholds := []int{95, 85, 75, 65, 55}

	// 第一步：一次性扫描所有图片并分析
	fmt.Println("\n🔍 开始扫描图片...")
	allWallpapers, err := scanAllImages(config)
	if err != nil {
		log.Fatalf("扫描失败: %v", err)
	}

	fmt.Printf("\n✅ 扫描完成！共发现 %d 张图片（分辨率/大小不达标者将放入 other）\n", len(allWallpapers))

	if len(allWallpapers) == 0 {
		fmt.Println("❌ 未找到符合条件的图片")
		return
	}

	// 第二步：分类并移动/复制，每张图片只归入一个目录
	// 优先级：分辨率不达标→ other；达标者按 暗色 → 亮色 → 中性

	// 2.0 过小图片(长边<1080)放入独立的 fixed 目录；其余分辨率不达标的放入 other
	conforming := make([]*WallpaperInfo, 0, len(allWallpapers))
	for i := range allWallpapers {
		wp := &allWallpapers[i]
		// 过小图片单独归置，不按明暗/分辨率细分
		longEdge := wp.Width
		if wp.Height > longEdge {
			longEdge = wp.Height
		}
		if longEdge < 1080 {
			fixedDir := filepath.Join(config.TargetDir, "fixed")
			fmt.Printf("[fixed] 长边 %d 过小 → fixed/\n", longEdge)
			if err := moveOrCopyFile(wp, fixedDir, "", "", config.CopyMode); err != nil {
				log.Printf("⚠️  处理 fixed 失败 %s: %v", wp.Path, err)
			}
			continue
		}
		if wp.Width < config.MinWidth || wp.Height < config.MinHeight {
			otherDir := filepath.Join(config.TargetDir, "other")
			fmt.Printf("[other] 分辨率不足 %dx%d  → other/\n", wp.Width, wp.Height)
			if err := moveOrCopyFile(wp, otherDir, directionName(wp.Width, wp.Height), resolutionTierName(wp.Width, wp.Height), config.CopyMode); err != nil {
				log.Printf("⚠️  处理 other 失败 %s: %v", wp.Path, err)
			}
			continue
		}
		conforming = append(conforming, wp)
	}

	// 第二步：暗色图片 → dark_N（从高到低，每张只归入最高档）
	for _, threshold := range thresholds {
		minScore := float64(threshold) / 100.0
		targetDir := filepath.Join(config.TargetDir, fmt.Sprintf("dark_%d", threshold))
		fmt.Printf("\n📁 处理暗色等级 ≥ %d%% 的图片\n", threshold)
		moved := 0
		remaining := conforming[:0]
		for _, wp := range conforming {
			if wp.DarkScore >= minScore {
				if err := moveOrCopyFile(wp, targetDir, directionName(wp.Width, wp.Height), resolutionTierName(wp.Width, wp.Height), config.CopyMode); err != nil {
					log.Printf("⚠️  移动失败 %s: %v", wp.Path, err)
				} else {
					moved++
				}
			} else {
				remaining = append(remaining, wp)
			}
		}
		fmt.Printf("   ✅ 暗色 ≥ %d%%：移动 %d 张\n", threshold, moved)
		conforming = remaining
	}

	// 第三步：亮色图片 → bright_N（从高到低）
	for _, threshold := range thresholds {
		minScore := float64(threshold) / 100.0
		targetDir := filepath.Join(config.TargetDir, fmt.Sprintf("bright_%d", threshold))
		fmt.Printf("\n📁 处理亮色等级 ≥ %d%% 的图片\n", threshold)
		moved := 0
		remaining := conforming[:0]
		for _, wp := range conforming {
			if wp.LightScore >= minScore {
				if err := moveOrCopyFile(wp, targetDir, directionName(wp.Width, wp.Height), resolutionTierName(wp.Width, wp.Height), config.CopyMode); err != nil {
					log.Printf("⚠️  移动失败 %s: %v", wp.Path, err)
				} else {
					moved++
				}
			} else {
				remaining = append(remaining, wp)
			}
		}
		fmt.Printf("   ✅ 亮色 ≥ %d%%：移动 %d 张\n", threshold, moved)
		conforming = remaining
	}

	// 第四步：剩余为中性图片 → neutral
	moved := 0
	neutralDir := filepath.Join(config.TargetDir, "neutral")
	for _, wp := range conforming {
		if err := moveOrCopyFile(wp, neutralDir, directionName(wp.Width, wp.Height), resolutionTierName(wp.Width, wp.Height), config.CopyMode); err != nil {
			log.Printf("⚠️  移动失败 %s: %v", wp.Path, err)
		} else {
			moved++
		}
	}
	if moved > 0 {
		fmt.Printf("\n📁 处理中性色图片 → neutral/：移动 %d 张\n", moved)
	}

	fmt.Printf("\n🎉 所有操作完成！文件已整理到: %s/\n", config.TargetDir)
}

// SizeFilterError 表示文件大小不达标（min-size / max-size）
type SizeFilterError struct {
	Path   string
	Reason string
}

func (e *SizeFilterError) Error() string { return e.Reason }

// DecodeError 表示文件无法打开或解码（损坏/非图片）
type DecodeError struct {
	Path   string
	Reason string
}

func (e *DecodeError) Error() string { return e.Reason }

// moveToErrorDir 将处理异常的文件移动到 target/error 目录
func moveToErrorDir(path string, config Config, err error) {
	errDir := filepath.Join(config.TargetDir, "error")
	if err := os.MkdirAll(errDir, 0755); err != nil {
		log.Printf("⚠️  创建 error 目录失败 %s: %v", errDir, err)
		return
	}

	ext := filepath.Ext(path)
	dst := filepath.Join(errDir, newUUID()+ext)
	// 文件名冲突：追加序号
	for i := 1; ; i++ {
		if _, e := os.Stat(dst); os.IsNotExist(e) {
			break
		}
		dst = filepath.Join(errDir, fmt.Sprintf("%s_(%d)%s", newUUID(), i, ext))
	}

	reason := "unknown"
	switch err.(type) {
	case *SizeFilterError:
		reason = "size"
	case *DecodeError:
		reason = "decode"
	}

	if mErr := moveFile(path, dst); mErr != nil {
		log.Printf("⚠️  移动文件到 error 目录失败 %s: %v", path, mErr)
		return
	}
	fmt.Printf("   → error/%s: %s (%s)\n", reason, filepath.Base(dst), err.Error())
}

// analyzeImageFile 分析单个图片的黑暗/亮度程度（不做阈值过滤，返回完整信息）
func analyzeImageFile(path string, config Config) (*WallpaperInfo, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	sizeKB := fileInfo.Size() / 1024
	if sizeKB < config.MinSizeKB {
		return nil, &SizeFilterError{Path: path, Reason: fmt.Sprintf("文件太小: %dKB < %dKB", sizeKB, config.MinSizeKB)}
	}

	if fileInfo.Size() > config.MaxSizeMB*1024*1024 {
		return nil, &SizeFilterError{Path: path, Reason: fmt.Sprintf("文件太大: %dMB > %dMB", fileInfo.Size()/1024/1024, config.MaxSizeMB)}
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, &DecodeError{Path: path, Reason: err.Error()}
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, &DecodeError{Path: path, Reason: err.Error()}
	}

	darkScore, lightScore, avgColor, width, height := analyzeImage(img)

	return &WallpaperInfo{
		Path:       path,
		DarkScore:  darkScore,
		LightScore: lightScore,
		AvgColor:   avgColor,
		Size:       fileInfo.Size(),
		Width:      width,
		Height:     height,
	}, nil
}

// analyzeImage 分析图片的黑暗程度、亮度程度和尺寸
// darkScore: 暗色像素（亮度 < 80）占比；lightScore: 亮色像素（亮度 > 175）占比
func analyzeImage(img image.Image) (darkScore float64, lightScore float64, avgColor color.RGBA, width, height int) {
	bounds := img.Bounds()
	width = bounds.Dx()
	height = bounds.Dy()
	totalPixels := width * height

	if totalPixels == 0 {
		return 0, 0, color.RGBA{0, 0, 0, 255}, width, height
	}

	var sumR, sumG, sumB uint64
	darkCount := 0
	lightCount := 0
	sampleCount := 0

	// 大图采样以提高性能
	sampleStep := 1
	if totalPixels > 1_000_000 {
		sampleStep = int(math.Sqrt(float64(totalPixels) / 100_000))
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y += sampleStep {
		for x := bounds.Min.X; x < bounds.Max.X; x += sampleStep {
			r, g, b, _ := img.At(x, y).RGBA()
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)
			sumR += uint64(r8)
			sumG += uint64(g8)
			sumB += uint64(b8)

			// 亮度计算（ITU-R BT.601）
			brightness := 0.299*float64(r8) + 0.587*float64(g8) + 0.114*float64(b8)
			if brightness < 80 {
				darkCount++
			} else if brightness > 175 {
				lightCount++
			}
			sampleCount++
		}
	}

	if sampleCount == 0 {
		return 0, 0, color.RGBA{0, 0, 0, 255}, width, height
	}

	darkScore = float64(darkCount) / float64(sampleCount)
	lightScore = float64(lightCount) / float64(sampleCount)

	avgR := uint8(sumR / uint64(sampleCount))
	avgG := uint8(sumG / uint64(sampleCount))
	avgB := uint8(sumB / uint64(sampleCount))

	return darkScore, lightScore, color.RGBA{avgR, avgG, avgB, 255}, width, height
}

// scanAllImages 一次性扫描所有图片并分析黑暗程度
func scanAllImages(config Config) ([]WallpaperInfo, error) {
	var allWallpapers []WallpaperInfo
	var mu sync.Mutex

	jobs := make(chan string, 100)
	results := make(chan *WallpaperInfo, 100)

	// 预计算目标目录的绝对路径，遍历时跳过
	absTarget, _ := filepath.Abs(config.TargetDir)

	// 启动 worker 池
	var workerWg sync.WaitGroup
	for i := 0; i < config.Parallel; i++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()
			for path := range jobs {
				if config.Verbose {
					fmt.Printf("Worker %d 处理: %s\n", workerID, filepath.Base(path))
				}
				wp, err := analyzeImageFile(path, config)
				if err != nil {
					if config.Verbose {
						fmt.Printf("⚠️ 处理异常 %s: %v -> error/\n", filepath.Base(path), err)
					}
					moveToErrorDir(path, config, err)
					continue
				}
				results <- wp
			}
		}(i)
	}

	// 结果收集 goroutine（用 WaitGroup 保证完成）
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		for wp := range results {
			mu.Lock()
			allWallpapers = append(allWallpapers, *wp)
			mu.Unlock()
		}
	}()

	// 遍历源目录，发送任务
	var processedCount int
	err := filepath.WalkDir(config.SourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// 跳过目标目录（避免重复处理之前的结果）
			if absTarget != "" {
				absPath, _ := filepath.Abs(path)
				if absPath == absTarget || strings.HasPrefix(absPath, absTarget+string(os.PathSeparator)) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if supportedExts[ext] {
			processedCount++
			jobs <- path
		}
		return nil
	})

	close(jobs)
	workerWg.Wait()
	close(results)
	resultWg.Wait() // 等待结果收集完成后再排序

	// 按黑暗程度排序（从高到低）
	sort.Slice(allWallpapers, func(i, j int) bool {
		return allWallpapers[i].DarkScore > allWallpapers[j].DarkScore
	})

	fmt.Printf("   扫描文件总数: %d\n", processedCount)
	fmt.Printf("   符合分辨率/大小要求: %d\n", len(allWallpapers))

	return allWallpapers, err
}

// resolutionTierName 根据图片较长边返回分辨率档位名称（如 "1K"、"4K"、"8K")
// 长边 < 1080 的过小图片单独归类为 "fixed"
func resolutionTierName(width, height int) string {
	longEdge := width
	if height > longEdge {
		longEdge = height
	}
	// 过小图片单独归类
	if longEdge < 1080 {
		return "fixed"
	}
	// 以标准横向分辨率作为各档位下限，取长边命中的最高档
	tiers := []struct {
		name string
		edge int
	}{
		{"8K", 7680},
		{"7K", 7168},
		{"6K", 6144},
		{"5K", 5120},
		{"4K", 3840},
		{"3K", 3072},
		{"2K", 1920},
		{"1K", 1080},
	}
	for _, t := range tiers {
		if longEdge >= t.edge {
			return t.name
		}
	}
	return "1K"
}

// newUUID 通过 crypto/rand 生成的 32 位十六进制随机文件名（不含扩展名）
func newUUID() string {
	const hexChars = "0123456789abcdef"
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 极难发生；回退到时间戳保证唯一
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	for i, b := range buf {
		buf[i] = hexChars[b&0x0f]
	}
	return string(buf)
}

// directionName 返回图像的横竖方向（宽==高为 square，宽>高为 horizontal，否则 vertical）
func directionName(width, height int) string {
	if width == height {
		return "square"
	}
	if width > height {
		return "horizontal"
	}
	return "vertical"
}

// moveOrCopyFile 移动或复制文件到目标目录（direction 与 tier 为子目录，可为空）
func moveOrCopyFile(wp *WallpaperInfo, targetDir, direction, tier string, copyMode bool) error {
	dir := targetDir
	if direction != "" {
		dir = filepath.Join(dir, direction)
	}
	if tier != "" {
		dir = filepath.Join(dir, tier)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	ext := filepath.Ext(wp.Path)
	newFilename := newUUID() + ext
	dstPath := filepath.Join(dir, newFilename)

	// 文件名冲突：追加序号
	if _, err := os.Stat(dstPath); err == nil {
		for i := 1; i < 100; i++ {
			newFilename = fmt.Sprintf("%s_(%d)%s", newUUID(), i, ext)
			dstPath = filepath.Join(dir, newFilename)
			if _, err := os.Stat(dstPath); os.IsNotExist(err) {
				break
			}
		}
	}

	if copyMode {
		return copyFile(wp.Path, dstPath)
	}
	return moveFile(wp.Path, dstPath)
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = dstFile.ReadFrom(srcFile)
	return err
}

// moveFile 移动文件（优先 rename，跨分区时复制后删除）
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
