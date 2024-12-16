# Kenaito Dark Wallpaper Finder 🖤

一个自动扫描、分析并整理暗色系壁纸的命令行工具。

程序会递归扫描指定目录下的所有图片，根据亮度算法计算每张图片的"黑暗程度"评分，然后按评分等级自动分类移动（或复制）到不同子目录中。

## 功能特性

- **智能暗色分析**：基于 ITU-R BT.601 亮度公式计算黑暗评分，纯黑加分，鲜艳色惩罚
- **多格式支持**：JPG、PNG、GIF、BMP、WebP、TIFF
- **五级分类**：按 ≥95%、≥85%、≥75%、≥65%、≥55% 五个黑暗等级自动归档
- **并发处理**：默认 24 线程并行分析，大图自动采样加速
- **灵活配置**：所有参数均可通过命令行调整

## 编译

```bash
# Linux / macOS
go build -o kenaito-dark-wallpaper-finder main.go

# Windows
go build -o kenaito-dark-wallpaper-finder.exe main.go
```

或使用构建脚本：

```bash
bash build.sh
```

## 快速开始

```bash
# 在壁纸目录下运行（默认移动模式，等待5秒确认）
cd ~/Pictures/wallpapers
./kenaito-dark-wallpaper-finder

# 用复制模式试运行（不会移动原文件）
./kenaito-dark-wallpaper-finder -copy -sleep 0
```

执行后，源目录下会生成如下结构：

```
wallpapers/
├── wallpapers_dark_wallpapers/
│   ├── dark_95/    ← 黑暗度 ≥ 95%
│   │   ├── photo1_[2560x1440]_dark97.jpg
│   │   └── ...
│   ├── dark_85/    ← 黑暗度 ≥ 85%
│   ├── dark_75/    ← 黑暗度 ≥ 75%
│   ├── dark_65/    ← 黑暗度 ≥ 65%
│   └── dark_55/    ← 黑暗度 ≥ 55%
├── bright_wallpaper.jpg
└── ...
```

## 命令行参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `-source` | string | `.` | 源目录路径，递归扫描该目录下所有图片 |
| `-target` | string | 自动生成 | 目标目录基名，留空则自动使用 `<源目录名>_dark_wallpapers` |
| `-min-width` | int | `1920` | 图片最小宽度（像素），低于此值跳过 |
| `-min-height` | int | `1080` | 图片最小高度（像素），低于此值跳过 |
| `-min-size` | int | `100` | 最小文件大小（KB），小于此值跳过 |
| `-max-size` | int | `100` | 最大文件大小（MB），大于此值跳过（排除异常大文件） |
| `-copy` | bool | `false` | 复制模式，设为 true 则复制原文件而非移动 |
| `-parallel` | int | `24` | 并发分析线程数，根据 CPU 核心数调整 |
| `-verbose` | bool | `true` | 详细输出，显示每个文件的处理过程 |
| `-sleep` | int | `5` | 执行前等待秒数，设为 0 则立即执行。等待期间可按 Ctrl+C 取消 |

## 使用示例

### 基础用法

```bash
# 扫描当前目录，移动暗色壁纸到子目录（默认行为）
./kenaito-dark-wallpaper-finder

# 扫描指定目录
./kenaito-dark-wallpaper-finder -source ~/Downloads/wallpapers

# 复制模式（保留原文件不动）
./kenaito-dark-wallpaper-finder -source ~/Pictures -copy

# 跳过等待，直接执行
./kenaito-dark-wallpaper-finder -sleep 0
```

### 自定义筛选条件

```bash
# 只要 4K 壁纸（3840x2160 以上）
./kenaito-dark-wallpaper-finder -min-width 3840 -min-height 2160

# 只处理 10MB 以上的大图
./kenaito-dark-wallpaper-finder -min-size 10240

# 只处理 500KB ~ 5MB 之间的图片
./kenaito-dark-wallpaper-finder -min-size 500 -max-size 5
```

### 性能调优

```bash
# 低配机器，减少并发数
./kenaito-dark-wallpaper-finder -parallel 4

# 高配机器，关闭详细输出加速
./kenaito-dark-wallpaper-finder -parallel 48 -verbose=false
```

### 组合使用

```bash
# 扫描外接硬盘上的壁纸，复制模式，4K 以上，不等待，静默执行
./kenaito-dark-wallpaper-finder \
  -source /Volumes/ExternalHD/Wallpapers \
  -target ~/Pictures/DarkWallpapers \
  -copy \
  -min-width 3840 \
  -min-height 2160 \
  -sleep 0 \
  -verbose=false
```

## 工作原理

1. **扫描阶段**：递归遍历源目录，识别支持格式的图片文件，按分辨率和文件大小初步筛选
2. **分析阶段**：多线程解码图片，对大图自动采样（>100万像素时按 sqrt(N/10万) 步长采样），通过 ITU-R BT.601 亮度公式计算黑暗评分
3. **分类阶段**：按五个阈值等级（95%、85%、75%、65%、55%）筛选，移动到对应子目录。每个文件只归入其最高匹配的等级目录

### 黑暗评分算法

```
brightness = 0.299×R + 0.587×G + 0.114×B
darkScore = (1 - brightness/255) ^ 0.6
```

- 纯黑像素（RGB < 40）额外加成 ×1.3
- 鲜艳像素（任一通道 > 200）惩罚 ×0.7

## 注意事项

- 默认使用**移动**模式，原文件会从源目录移走。首次使用建议加 `-copy` 试运行
- 目标目录会被自动跳过，不会重复处理之前已整理的文件
- 文件名冲突时自动追加序号 `_(1)`、`_(2)` 等
- 支持的图片格式依赖 Go 标准库 + `golang.org/x/image`：JPG、PNG、GIF、BMP、WebP、TIFF

## License

MIT
