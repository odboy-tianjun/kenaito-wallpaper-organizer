package main

import (
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
)

// 支持的图片格式
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
	SourceDir     string  // 源目录，如果是当前目录，设置为 "."
	TargetDir     string  // 移动或复制到的目标目录，不建议设置为 "."，因为那不是脱了裤子放屁吗
	DarkThreshold float64 // 黑暗程度评分（0-1，越高越暗）
	MinWidth      int     // 图片最小宽度
	MinHeight     int     // 图片最大宽度
	MinSizeKB     int64   // 文件大小最小值
	MaxSizeMB     int64   // 文件大小最大值
	CopyMode      bool    // true=复制, false=移动
	Parallel      int     // 并发数
	Verbose       bool    // 是否打印日志
}

// 默认配置
func DefaultConfig() Config {
	return Config{
		SourceDir:     ".",
		TargetDir:     "./dark_wallpapers",
		DarkThreshold: 0.75,
		MinWidth:      1920,
		MinHeight:     1080,
		MinSizeKB:     100,   // 最小100KB
		MaxSizeMB:     100,   // 最大100MB
		CopyMode:      false, // 默认移动
		Parallel:      24,    // i5-12490f配置
		Verbose:       true,
	}
}

// 处理单个图片文件
func processImageFile(path string, config Config) (*WallpaperInfo, error) {
	// 检查文件大小
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

	// 打开图片文件
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 解码图片
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	// 分析图片
	darkScore, avgColor, width, height := analyzeImage(img)

	// 检查分辨率
	if width < config.MinWidth || height < config.MinHeight {
		return nil, fmt.Errorf("分辨率不足: %dx%d < %dx%d", width, height, config.MinWidth, config.MinHeight)
	}

	// 检查黑暗程度
	if darkScore < config.DarkThreshold {
		return nil, fmt.Errorf("不够黑暗: %.2f < %.2f", darkScore, config.DarkThreshold)
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

// 移动或复制文件到目标目录
func moveOrCopyFile(wp *WallpaperInfo, config Config) error {
	// 创建目标目录
	err := os.MkdirAll(config.TargetDir, 0755)
	if err != nil {
		return err
	}

	// 生成目标文件名（保留原文件名，添加黑暗评分）
	ext := filepath.Ext(wp.Path)
	baseName := strings.TrimSuffix(filepath.Base(wp.Path), ext)

	// 清理文件名中的特殊字符
	cleanName := cleanFileName(baseName)

	// 添加分辨率信息
	newFilename := fmt.Sprintf("%s_[%dx%d]_dark%.0f%s",
		cleanName, wp.Width, wp.Height, wp.DarkScore*100, ext)
	dstPath := filepath.Join(config.TargetDir, newFilename)

	// 检查目标文件是否已存在
	if _, err := os.Stat(dstPath); err == nil {
		// 文件已存在，添加序号
		for i := 1; i < 100; i++ {
			newFilename = fmt.Sprintf("%s_[%dx%d]_dark%.0f_(%d)%s",
				cleanName, wp.Width, wp.Height, wp.DarkScore*100, i, ext)
			dstPath = filepath.Join(config.TargetDir, newFilename)
			if _, err := os.Stat(dstPath); os.IsNotExist(err) {
				break
			}
		}
	}

	if config.CopyMode {
		// 复制文件
		return copyFile(wp.Path, dstPath)
	} else {
		// 移动文件
		return moveFile(wp.Path, dstPath)
	}
}

// 清理文件名
func cleanFileName(name string) string {
	// 替换或移除特殊字符
	replacements := map[string]string{
		" ": "_", "/": "_", "\\": "_", ":": "_",
		"*": "", "?": "", "\"": "", "<": "", ">": "", "|": "",
	}

	result := name
	for old, n := range replacements {
		result = strings.ReplaceAll(result, old, n)
	}

	// 限制长度
	if len(result) > 100 {
		result = result[:100]
	}

	return result
}

// 复制文件
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

// 移动文件
func moveFile(src, dst string) error {
	// 先尝试重命名（如果在同一分区）
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// 如果重命名失败，则复制后删除
	if err := copyFile(src, dst); err != nil {
		return err
	}

	// 删除原文件
	return os.Remove(src)
}

// 显示结果摘要
func printSummary(wallpapers []WallpaperInfo, config Config) {
	if len(wallpapers) == 0 {
		fmt.Println("❌ 未找到符合条件的黑色壁纸")
		return
	}

	fmt.Println("\n🎯 找到的黑色调壁纸:")
	fmt.Println(strings.Repeat("=", 60))

	for i, wp := range wallpapers {
		if i >= 20 { // 最多显示20个
			fmt.Printf("... 还有 %d 个未显示\n", len(wallpapers)-20)
			break
		}

		action := "移动到"
		if config.CopyMode {
			action = "复制到"
		}

		fmt.Printf("%2d. %s\n", i+1, filepath.Base(wp.Path))
		fmt.Printf("    原路径: %s\n", wp.Path)
		fmt.Printf("    %s: %s\n", action, config.TargetDir)
		fmt.Printf("    黑暗程度: %.1f%%\n", wp.DarkScore*100)
		fmt.Printf("    平均颜色: RGB(%3d, %3d, %3d)\n", wp.AvgColor.R, wp.AvgColor.G, wp.AvgColor.B)
		fmt.Printf("    分辨率: %d × %d\n", wp.Width, wp.Height)
		fmt.Printf("    文件大小: %.1f MB\n", float64(wp.Size)/1024/1024)

		// 黑暗程度可视化
		bars := int(wp.DarkScore * 20)
		fmt.Printf("    [")
		for j := 0; j < 20; j++ {
			if j < bars {
				fmt.Printf("█")
			} else {
				fmt.Printf(" ")
			}
		}
		fmt.Printf("]\n\n")
	}
}

// 保存结果到日志文件
func saveResultsToLog(wallpapers []WallpaperInfo, config Config) {
	logFile := filepath.Join(config.TargetDir, "dark_wallpapers_log.txt")
	file, err := os.Create(logFile)
	if err != nil {
		log.Printf("无法创建日志文件: %v", err)
		return
	}
	defer file.Close()

	fmt.Fprintf(file, "黑色壁纸处理日志 - %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(file, "源目录: %s\n", config.SourceDir)
	fmt.Fprintf(file, "目标目录: %s\n", config.TargetDir)
	fmt.Fprintf(file, "黑暗阈值: %.2f\n", config.DarkThreshold)
	fmt.Fprintf(file, "找到壁纸数量: %d\n\n", len(wallpapers))

	for i, wp := range wallpapers {
		fmt.Fprintf(file, "%d. %s\n", i+1, filepath.Base(wp.Path))
		fmt.Fprintf(file, "   黑暗程度: %.1f%%\n", wp.DarkScore*100)
		fmt.Fprintf(file, "   分辨率: %dx%d\n", wp.Width, wp.Height)
		fmt.Fprintf(file, "   大小: %.2f MB\n", float64(wp.Size)/1024/1024)
		fmt.Fprintf(file, "   原路径: %s\n", wp.Path)
		fmt.Fprintf(file, "   目标: %s/%s\n\n", config.TargetDir, filepath.Base(wp.Path))
	}

	fmt.Printf("📝 详细日志已保存到: %s\n", logFile)
}

// 分析图片的黑暗程度和尺寸
func analyzeImage(img image.Image) (darkScore float64, avgColor color.RGBA, width, height int) {
	bounds := img.Bounds()
	width = bounds.Dx()
	height = bounds.Dy()
	totalPixels := width * height

	if totalPixels == 0 {
		return 0, color.RGBA{0, 0, 0, 255}, width, height
	}

	var sumR, sumG, sumB uint64

	// 采样分析（对于大图片进行采样以提高性能）
	sampleStep := 1
	if totalPixels > 1000000 { // 超过100万像素则采样
		sampleStep = int(math.Sqrt(float64(totalPixels) / 100000))
	}

	sampleCount := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += sampleStep {
		for x := bounds.Min.X; x < bounds.Max.X; x += sampleStep {
			r, g, b, _ := img.At(x, y).RGBA()
			sumR += uint64(r >> 8)
			sumG += uint64(g >> 8)
			sumB += uint64(b >> 8)
			sampleCount++
		}
	}

	if sampleCount == 0 {
		return 0, color.RGBA{0, 0, 0, 255}, width, height
	}

	avgR := uint8(sumR / uint64(sampleCount))
	avgG := uint8(sumG / uint64(sampleCount))
	avgB := uint8(sumB / uint64(sampleCount))

	// 计算黑暗程度评分
	brightness := 0.299*float64(avgR) + 0.587*float64(avgG) + 0.114*float64(avgB)
	darkScore = 1.0 - (brightness / 255.0)

	// 增强黑色权重
	darkScore = math.Pow(darkScore, 0.6)

	// 如果平均颜色值很低，进一步提高评分
	if avgR < 40 && avgG < 40 && avgB < 40 {
		darkScore = math.Min(1.0, darkScore*1.3)
	}

	// 惩罚太鲜艳的颜色
	if avgR > 200 || avgG > 200 || avgB > 200 {
		darkScore *= 0.7
	}

	return darkScore, color.RGBA{avgR, avgG, avgB, 255}, width, height
}

// 查找并移动黑色壁纸
func findAndMoveDarkWallpapers(config Config) ([]WallpaperInfo, error) {
	var darkWallpapers []WallpaperInfo
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 创建worker池
	jobs := make(chan string, 100)
	results := make(chan *WallpaperInfo, 100)
	errors := make(chan error, 100)

	// 启动worker
	for i := 0; i < config.Parallel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for path := range jobs {
				if config.Verbose {
					fmt.Printf("Worker %d 处理: %s\n", workerID, filepath.Base(path))
				}

				wp, err := processImageFile(path, config)
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

	// 收集结果并移动文件
	var processedCount int
	var movedCount int
	go func() {
		for wp := range results {
			mu.Lock()
			darkWallpapers = append(darkWallpapers, *wp)
			mu.Unlock()

			// 移动文件
			if err := moveOrCopyFile(wp, config); err != nil {
				errors <- fmt.Errorf("移动文件失败 %s: %v", wp.Path, err)
				continue
			}

			movedCount++
			if config.Verbose {
				fmt.Printf("✓ 已处理: %s (黑暗度: %.1f%%)\n",
					filepath.Base(wp.Path), wp.DarkScore*100)
			}
		}
	}()

	// 收集错误
	var processErrors []error
	go func() {
		for err := range errors {
			processErrors = append(processErrors, err)
		}
	}()

	// 遍历源目录，发送任务
	err := filepath.WalkDir(config.SourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			// 跳过目标目录
			if filepath.Clean(path) == filepath.Clean(config.TargetDir) {
				return filepath.SkipDir
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
	wg.Wait()
	close(results)
	close(errors)

	// 按黑暗程度排序
	sort.Slice(darkWallpapers, func(i, j int) bool {
		return darkWallpapers[i].DarkScore > darkWallpapers[j].DarkScore
	})

	// 打印统计信息
	fmt.Printf("\n📊 处理完成统计:\n")
	fmt.Printf("   扫描文件总数: %d\n", processedCount)
	fmt.Printf("   找到黑色壁纸: %d\n", len(darkWallpapers))
	fmt.Printf("   成功移动/复制: %d\n", movedCount)
	if len(processErrors) > 0 {
		fmt.Printf("   错误数量: %d\n", len(processErrors))
	}

	if len(processErrors) > 0 && config.Verbose {
		fmt.Println("\n⚠️ 错误列表:")
		for _, err := range processErrors {
			fmt.Printf("   %v\n", err)
		}
	}

	return darkWallpapers, err
}

func main() {
	// 配置
	config := DefaultConfig()

	// 可以在这里修改配置
	config.SourceDir = "."                 // 源目录，包含壁纸
	config.TargetDir = "./dark_wallpapers" // 目标目录
	config.DarkThreshold = 0.75            // 黑暗阈值
	config.MinWidth = 1024
	config.MinHeight = 728
	config.CopyMode = false // false=移动，true=复制
	config.Verbose = true

	fmt.Println("🖤 黑色壁纸查找与移动工具")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("源目录: %s\n", config.SourceDir)
	fmt.Printf("目标目录: %s\n", config.TargetDir)
	fmt.Printf("黑暗阈值: %.2f\n", config.DarkThreshold)
	fmt.Printf("最小分辨率: %dx%d\n", config.MinWidth, config.MinHeight)
	fmt.Printf("模式: %s\n", map[bool]string{true: "复制", false: "移动"}[config.CopyMode])
	fmt.Println()

	// 检查源目录是否存在
	if _, err := os.Stat(config.SourceDir); os.IsNotExist(err) {
		log.Fatalf("源目录不存在: %s", config.SourceDir)
	}

	// 执行查找和移动
	wallpapers, err := findAndMoveDarkWallpapers(config)
	if err != nil {
		log.Fatalf("处理失败: %v", err)
	}

	// 显示结果
	printSummary(wallpapers, config)

	// 保存结果到日志文件
	if len(wallpapers) > 0 {
		saveResultsToLog(wallpapers, config)
	}
}
