package main

import (
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
	Path      string
	DarkScore float64 // 黑暗程度评分（0-1，越高越暗）
	AvgColor  color.RGBA
	Size      int64
	Width     int
	Height    int
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
		MinWidth:     1920,
		MinHeight:    1080,
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

	fmt.Println("🖤 黑色壁纸查找与移动工具")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("  源目录:     %s\n", config.SourceDir)
	fmt.Printf("  目标目录:   %s（按黑暗等级分子目录）\n", config.TargetDir)
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

	fmt.Printf("\n✅ 扫描完成！共 %d 张图片满足分辨率和大小要求\n", len(allWallpapers))

	if len(allWallpapers) == 0 {
		fmt.Println("❌ 未找到符合条件的图片")
		return
	}

	// 第二步：按黑暗等级分类并移动到不同子目录（从高到低，每张图片只归入一个等级）
	for _, threshold := range thresholds {
		minScore := float64(threshold) / 100.0
		targetDir := filepath.Join(config.TargetDir, fmt.Sprintf("dark_%d", threshold))

		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("📁 处理黑暗等级 ≥ %d%% 的图片\n", threshold)
		fmt.Printf("   目标目录: %s\n", targetDir)
		fmt.Println(strings.Repeat("=", 60))

		filtered := filterByDarkness(allWallpapers, minScore)

		if len(filtered) == 0 {
			fmt.Printf("   ⚠️  没有符合黑暗等级 ≥ %d%% 的图片\n", threshold)
			continue
		}

		fmt.Printf("   找到 %d 张符合条件的图片\n", len(filtered))

		movedCount, moveErr := moveWallpapers(filtered, targetDir, config.CopyMode)
		if moveErr != nil {
			log.Printf("⚠️  移动部分文件时出错: %v", moveErr)
		}

		fmt.Printf("   ✓ 成功移动/复制 %d 张图片\n", movedCount)

		// 从待处理列表中移除本轮已处理的图片，避免后续低阈值重复处理
		processed := make(map[string]struct{}, len(filtered))
		for _, wp := range filtered {
			processed[wp.Path] = struct{}{}
		}
		remaining := make([]WallpaperInfo, 0, len(allWallpapers))
		for _, wp := range allWallpapers {
			if _, ok := processed[wp.Path]; !ok {
				remaining = append(remaining, wp)
			}
		}
		allWallpapers = remaining
	}

	fmt.Printf("\n🎉 所有操作完成！文件已整理到: %s/\n", config.TargetDir)
}

// analyzeImageFile 分析单个图片的黑暗程度（不做阈值过滤）
func analyzeImageFile(path string, config Config) (*WallpaperInfo, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	sizeKB := fileInfo.Size() / 1024
	if sizeKB < config.MinSizeKB {
		return nil, fmt.Errorf("文件太小: %dKB < %dKB", sizeKB, config.MinSizeKB)
	}

	if fileInfo.Size() > config.MaxSizeMB*1024*1024 {
		return nil, fmt.Errorf("文件太大: %dMB > %dMB", fileInfo.Size()/1024/1024, config.MaxSizeMB)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	darkScore, avgColor, width, height := analyzeImage(img)

	if width < config.MinWidth || height < config.MinHeight {
		return nil, fmt.Errorf("分辨率不足: %dx%d < %dx%d", width, height, config.MinWidth, config.MinHeight)
	}

	return &WallpaperInfo{
		Path:      path,
		DarkScore: darkScore,
		AvgColor:  avgColor,
		Size:      fileInfo.Size(),
		Width:     width,
		Height:    height,
	}, nil
}

// analyzeImage 分析图片的黑暗程度和尺寸
// 使用暗色像素占比（亮度 < 80 的像素比例）作为评分，比平均亮度更能准确反映暗色背景壁纸
func analyzeImage(img image.Image) (darkScore float64, avgColor color.RGBA, width, height int) {
	bounds := img.Bounds()
	width = bounds.Dx()
	height = bounds.Dy()
	totalPixels := width * height

	if totalPixels == 0 {
		return 0, color.RGBA{0, 0, 0, 255}, width, height
	}

	var sumR, sumG, sumB uint64
	darkCount := 0
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

			// 亮度计算（ITU-R BT.601），判断是否为暗色像素
			brightness := 0.299*float64(r8) + 0.587*float64(g8) + 0.114*float64(b8)
			if brightness < 80 {
				darkCount++
			}
			sampleCount++
		}
	}

	if sampleCount == 0 {
		return 0, color.RGBA{0, 0, 0, 255}, width, height
	}

	// 暗色像素占比作为黑暗评分（0-1），比平均亮度更准确：
	// 一张 80% 黑色背景 + 20% 明亮主体的壁纸，评分 0.80，正确反映"暗色背景"特性
	darkScore = float64(darkCount) / float64(sampleCount)

	avgR := uint8(sumR / uint64(sampleCount))
	avgG := uint8(sumG / uint64(sampleCount))
	avgB := uint8(sumB / uint64(sampleCount))

	return darkScore, color.RGBA{avgR, avgG, avgB, 255}, width, height
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
						fmt.Printf("跳过 %s: %v\n", filepath.Base(path), err)
					}
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

// filterByDarkness 根据黑暗阈值筛选图片
func filterByDarkness(wallpapers []WallpaperInfo, minDarkScore float64) []WallpaperInfo {
	var filtered []WallpaperInfo
	for _, wp := range wallpapers {
		if wp.DarkScore >= minDarkScore {
			filtered = append(filtered, wp)
		}
	}
	return filtered
}

// cleanFileName 清理文件名中的特殊字符
func cleanFileName(name string) string {
	replacements := map[string]string{
		" ": "_", "/": "_", "\\": "_", ":": "_",
		"*": "", "?": "", "\"": "", "<": "", ">": "", "|": "",
	}
	result := name
	for old, n := range replacements {
		result = strings.ReplaceAll(result, old, n)
	}
	// 按 rune 截断，避免截断多字节 UTF-8 字符
	runes := []rune(result)
	if len(runes) > 100 {
		result = string(runes[:100])
	}
	return result
}

// moveOrCopyFile 移动或复制文件到目标目录
func moveOrCopyFile(wp *WallpaperInfo, targetDir string, copyMode bool) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	ext := filepath.Ext(wp.Path)
	baseName := strings.TrimSuffix(filepath.Base(wp.Path), ext)
	cleanName := cleanFileName(baseName)

	newFilename := fmt.Sprintf("%s_[%dx%d]_dark%.0f%s",
		cleanName, wp.Width, wp.Height, wp.DarkScore*100, ext)
	dstPath := filepath.Join(targetDir, newFilename)

	// 文件名冲突：追加序号
	if _, err := os.Stat(dstPath); err == nil {
		for i := 1; i < 100; i++ {
			newFilename = fmt.Sprintf("%s_[%dx%d]_dark%.0f_(%d)%s",
				cleanName, wp.Width, wp.Height, wp.DarkScore*100, i, ext)
			dstPath = filepath.Join(targetDir, newFilename)
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

// moveWallpapers 将图片列表移动到指定目标目录
func moveWallpapers(wallpapers []WallpaperInfo, targetDir string, copyMode bool) (int, error) {
	movedCount := 0
	var firstErr error

	for _, wp := range wallpapers {
		if err := moveOrCopyFile(&wp, targetDir, copyMode); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		movedCount++
	}

	return movedCount, firstErr
}
