# Kenaito Wallpaper Organizer 🖼️

一个自动扫描、分析并归类壁纸的命令行工具。它会递归扫描指定目录下的所有图片,分析每张图的**明暗程度**,并按**明暗等级 → 方向 → 分辨率**进行分类归档(移动或复制)。

## 功能特性

- **智能明暗分析**:基于 ITU-R BT.601 亮度公式,同时统计暗色像素(亮度 < 80)与亮色像素(亮度 > 175)占比
- **五级明暗分类**:暗色 `dark_95/85/75/65/55` 与亮色 `bright_95/85/75/65/55` 五档归档
- **方向区分**:横屏 `horizontal` / 竖屏 `vertical` / 方形 `square` 子目录
- **分辨率分档**:按长边区分 1K~8K 子目录
- **特殊归置**:过小图(`fixed`)、分辨率不足(`other`)、异常文件(`error`)
- **多格式支持**:JPG、PNG、GIF、BMP、WebP、TIFF
- **并发处理**:默认 24 线程并行分析,大图自动采样加速
- **UUID 命名**:归档后的文件统一重命名为随机 UUID,不保留原文件名

## 编译

```bash
# Linux / macOS
go build -o kenaito-wallpaper-organizer main.go

# Windows
go build -o kenaito-wallpaper-organizer.exe main.go
```

或使用构建脚本:

```bash
bash build.sh
```

## 快速开始

```bash
# 在壁纸目录下运行（默认移动模式，等待5秒确认）
cd ~/Pictures/wallpapers
./kenaito-wallpaper-organizer

# 用复制模式试运行（不会移动原文件）
./kenaito-wallpaper-organizer -copy -sleep 0
```

执行后,源目录下会生成如下结构:

├── wallpapers_wallpapers/
│   ├── dark_95/
│   │   ├── horizontal/  → 1K/ 2K/ 4K/ …
│   │   ├── vertical/    → 1K/ 2K/ 4K/ …
│   │   └── square/      → 1K/ …
│   ├── bright_95/          # 与 dark_ 同构
│   ├── neutral/             # 明暗皆不达标但尺寸合格
│   ├── fixed/               # 长边 < 1080 的过小图
│   ├── other/               # 尺寸不足 min-width/min-height
│   └── error/               # 大小超限或解码失败的异常文件
```

## 命令行参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `-source` | string | `.` | 源目录路径，递归扫描该目录下所有图片 |
| `-target` | string | 自动生成 | 目标目录基名，留空自动使用 `<源目录名>_wallpapers` |
| `-min-width` | int | `1080` | 图片最小宽度（像素），低于此值进入 other |
| `-min-height` | int | `720` | 图片最小高度（像素），低于此值进入 other |
| `-min-size` | int | `100` | 最小文件大小（KB），小于此值进入 error |
| `-max-size` | int | `100` | 最大文件大小（MB），大于此值进入 error |
| `-copy` | bool | `false` | 复制模式，设为 true 则复制原文件而非移动 |
| `-parallel` | int | `24` | 并发分析线程数 |
| `-verbose` | bool | `true` | 详细输出，显示每个文件的处理过程 |
| `-sleep` | int | `5` | 执行前等待秒数，设为 0 则立即执行，等待期间可按 Ctrl+C 取消 |

## 使用示例

```bash
# 扫描指定目录
./kenaito-wallpaper-organizer -source ~/Downloads/wallpapers

# 复制模式（保留原文件不动）
./kenaito-wallpaper-organizer -source ~/Pictures -copy

# 跳过等待，直接执行
./kenaito-wallpaper-organizer -sleep 0

# 只要 4K 以上壁纸
./kenaito-wallpaper-organizer -min-width 3840 -min-height 2160

# 高配机器，关闭详细输出加速
./kenaito-wallpaper-organizer -parallel 48 -verbose=false
```

## 分类优先级

1. **fixed**：长边 < 1080 的过小图片
2. **other**：尺寸达标(≥1080)但低于 `min-width/min-height` 阈值的图片
3. **dark_N**(从高到低,每张只归入最高档)
4. **bright_N**(同理,处理完 dark 后剩余图中再判亮色档)
5. **neutral**：上面均未命中的图片
6. **error**：文件大小超限或解码失败的异常文件

所有正常分类(dark/bright/neutral/other)内部均按 方向 → 分辨率 分子目录。

## 明暗评分算法

```
brightness = 0.299×R + 0.587×G + 0.114×B   (ITU-R BT.601)
darkScore  = 亮度 < 80 的像素占比
lightScore = 亮度 > 175 的像素占比
```

## 注意事项

- 默认使用**移动**模式,原文件会从源目录移走,首次使用建议加 `-copy` 试运行
- 目标目录会被自动跳过,不会重复处理之前已整理的文件
- UUID 命名,极端情况下重名会自动追加 `_(1)`、`_(2)` 等序号
- 支持的图片格式依赖 Go 标准库 + `golang.org/x/image`:JPG、PNG、GIF、BMP、WebP、TIFF

## License

MIT
