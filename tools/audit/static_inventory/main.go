package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type fileFinding struct {
	hash           string
	responsibility string
	finding        string
}

var reviewed = map[string]fileFinding{}

var excludedExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".woff": true, ".woff2": true, ".ttf": true, ".zip": true,
	".pdf": true, ".mp3": true, ".mp4": true,
}

func main() {
	frontend := flag.String("frontend", `D:\Project\WebStorm\mcmods-cn-frontend`, "frontend repository")
	output := flag.String("output", "docs/audit/full-project-audit/modules", "output directory")
	reviewLog := flag.String("review-log", "tools/audit/static_inventory/reviewed_files.tsv", "manually maintained review log")
	flag.Parse()

	backend, err := os.Getwd()
	must(err)
	must(loadReviewLog(*reviewLog))
	fmt.Printf("loaded %d manually reviewed file records\n", len(reviewed))
	must(os.MkdirAll(*output, 0o755))
	must(writeFileInventory(filepath.Join(*output, "FILE_COVERAGE_INVENTORY.md"), backend, *frontend))
	must(writeRouteInventory(filepath.Join(*output, "API_ROUTE_INVENTORY.md"), filepath.Join(backend, "internal", "httpapi", "server.go")))
}

func writeFileInventory(path, backend, frontend string) error {
	type repo struct{ name, root string }
	repos := []repo{{"backend", backend}, {"frontend", frontend}}
	var out strings.Builder
	out.WriteString("# 文件覆盖清单\n\n")
	out.WriteString("本清单由只读工具生成路径和行数。工具不会自动把源码标记为“已完成”；只有本轮实际进行过人工调用链审查的文件才列为已完成，其余第一方源码保持“审查中”。\n\n")
	var firstPartyFiles, completedFiles, excludedFiles, unknownFiles int
	var firstPartyLines, completedLines int
	for _, repo := range repos {
		files, err := gitFiles(repo.root)
		if err != nil {
			return err
		}
		out.WriteString("## " + repo.name + "\n\n")
		out.WriteString("| 文件 | 模块 | 行数 | 审查状态 | 主要职责 | 发现问题 |\n| --- | --- | ---: | --- | --- | --- |\n")
		for _, relative := range files {
			full := filepath.Join(repo.root, filepath.FromSlash(relative))
			lines := lineCount(full)
			status, responsibility, finding, firstParty := classify(repo.name, repo.root, relative)
			if firstParty {
				firstPartyFiles++
				firstPartyLines += lines
				if status == "已完成" {
					completedFiles++
					completedLines += lines
				} else if status == "无法确认" {
					unknownFiles++
				}
			} else if status == "排除" {
				excludedFiles++
			}
			fmt.Fprintf(&out, "| `%s` | %s | %d | %s | %s | %s |\n", escape(relative), escape(module(relative)), lines, status, escape(responsibility), escape(finding))
		}
		out.WriteString("\n")
	}
	coverage := float64(0)
	if firstPartyLines > 0 {
		coverage = float64(completedLines) * 100 / float64(firstPartyLines)
	}
	summary := fmt.Sprintf("## 统计\n\n| 指标 | 数量 |\n| --- | ---: |\n| 第一方文件 | %d |\n| 已完成人工审查 | %d |\n| 排除文件 | %d |\n| 无法确认 | %d |\n| 第一方代码/配置行数 | %d |\n| 已完成人工审查行数 | %d |\n| 按行人工覆盖率 | %.2f%% |\n\n", firstPartyFiles, completedFiles, excludedFiles, unknownFiles, firstPartyLines, completedLines, coverage)
	out.WriteString(summary)
	if completedFiles == firstPartyFiles && unknownFiles == 0 {
		out.WriteString("清单中的全部第一方代码/配置文件均已完成逐文件人工语义审查；未开始、审查中和无法确认均为0。排除项按清单理由不计入第一方代码/配置基数。\n")
	} else {
		out.WriteString("未标为已完成的第一方文件仍为“审查中”，因此本清单不能支持“100% 逐行人工审计已经完成”的声明。构建、测试和静态搜索覆盖全部源码，但不等同于人工语义审查。\n")
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func classify(repo, root, relative string) (status, responsibility, finding string, firstParty bool) {
	ext := strings.ToLower(filepath.Ext(relative))
	key := repo + "/" + filepath.ToSlash(relative)
	if item, ok := reviewed[key]; ok {
		currentHash, err := fileHash(filepath.Join(root, filepath.FromSlash(relative)))
		if err == nil && currentHash == item.hash {
			responsibility := item.responsibility
			if responsibility == "" {
				responsibility = responsibilityFor(relative)
			}
			return "已完成", responsibility, item.finding, true
		}
		return "审查中", responsibilityFor(relative), "审查后文件内容已变化，必须重新审查", true
	}
	if excludedExtensions[ext] {
		return "排除", "二进制/媒体资源", "非可执行第一方源码", false
	}
	if ext == ".svg" {
		return "排除", "矢量静态资源", "构建和引用覆盖；未逐路径人工审查", false
	}
	if strings.HasPrefix(relative, "docs/") || ext == ".md" {
		return "排除", "文档", "作为实现证据交叉阅读，不计第一方功能源码", false
	}
	if relative == "package-lock.json" || relative == "go.sum" {
		return "排除", "依赖锁文件", "依赖一致性检查覆盖", false
	}
	if relative == "next-env.d.ts" {
		return "排除", "框架生成声明", "自动生成", false
	}
	return "审查中", responsibilityFor(relative), "尚未逐行人工确认", true
}

func loadReviewLog(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 2 {
			return fmt.Errorf("invalid review log line: %q", line)
		}
		responsibility := ""
		finding := "无"
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			finding = strings.TrimSpace(parts[2])
		}
		if len(parts) == 4 {
			responsibility = strings.TrimSpace(parts[2])
			if strings.TrimSpace(parts[3]) != "" {
				finding = strings.TrimSpace(parts[3])
			}
		}
		reviewed[filepath.ToSlash(strings.TrimSpace(parts[0]))] = fileFinding{
			hash:           strings.ToLower(strings.TrimSpace(parts[1])),
			responsibility: responsibility,
			finding:        finding,
		}
	}
	return scanner.Err()
}

func fileHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum), nil
}

func responsibilityFor(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "_test.go") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec."):
		return "测试"
	case strings.Contains(lower, "database") || strings.Contains(lower, "schema") || strings.Contains(lower, "migration"):
		return "数据库/Schema"
	case strings.Contains(lower, "queue") || strings.Contains(lower, "nats") || strings.Contains(lower, "worker"):
		return "异步任务/队列"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "permission") || strings.Contains(lower, "role"):
		return "认证/权限"
	case strings.Contains(lower, "comment") || strings.Contains(lower, "community"):
		return "社区/评论"
	case strings.Contains(lower, "oss") || strings.Contains(lower, "file") || strings.Contains(lower, "log_share"):
		return "文件/OSS/日志"
	case strings.Contains(lower, "notification") || strings.Contains(lower, "message"):
		return "通知/通信"
	case strings.Contains(lower, "admin"):
		return "后台管理"
	case strings.Contains(lower, "mod") || strings.Contains(lower, "project") || strings.Contains(lower, "catalog"):
		return "项目/资料"
	case strings.HasPrefix(lower, "app/"):
		return "前端页面/组件"
	default:
		return "基础设施/通用"
	}
}

func module(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) >= 3 && parts[0] == "internal" {
		return strings.Join(parts[:2], "/")
	}
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "/")
	}
	return parts[0]
}

func writeRouteInventory(path, source string) error {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, source, nil, parser.AllErrors)
	if err != nil {
		return err
	}
	type route struct{ method, path, guard, permission, handler, location string }
	routes := make([]route, 0, 512)
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		parts := strings.SplitN(value, " ", 2)
		if len(parts) != 2 {
			return true
		}
		guard, permission, handler := routeExpression(call.Args[1])
		position := fset.Position(call.Pos())
		routes = append(routes, route{parts[0], parts[1], guard, permission, handler, fmt.Sprintf("server.go:%d", position.Line)})
		return true
	})
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path == routes[j].path {
			return routes[i].method < routes[j].method
		}
		return routes[i].path < routes[j].path
	})
	var out strings.Builder
	out.WriteString("# API 路由静态清单\n\n")
	out.WriteString("本清单从 `internal/httpapi/server.go` 的 Go AST 生成，列出全部注册路由及其外层认证守卫。对象级权限、限流、事务、表和副作用仍需进入 Handler 语义审查，不能仅凭本表断言安全。\n\n")
	out.WriteString("| 方法 | 路径 | 外层守卫 | 权限 | Handler | 注册位置 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, item := range routes {
		fmt.Fprintf(&out, "| %s | `%s` | %s | `%s` | `%s` | `%s` |\n", item.method, escape(item.path), item.guard, escape(item.permission), escape(item.handler), item.location)
	}
	fmt.Fprintf(&out, "\n合计：%d 条路由。\n", len(routes))
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func routeExpression(expr ast.Expr) (guard, permission, handler string) {
	handler = deepestName(expr)
	guard = "public"
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return guard, "", handler
	}
	name := deepestName(call.Fun)
	switch name {
	case "requireAuth", "optionalAuth":
		guard = name
	case "requirePermission":
		guard = name
		if len(call.Args) > 0 {
			if value, ok := call.Args[0].(*ast.BasicLit); ok {
				permission, _ = strconv.Unquote(value.Value)
			}
		}
	}
	if len(call.Args) > 0 {
		handler = deepestName(call.Args[len(call.Args)-1])
	}
	return guard, permission, handler
}

func deepestName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	case *ast.CallExpr:
		if len(value.Args) > 0 {
			return deepestName(value.Args[len(value.Args)-1])
		}
		return deepestName(value.Fun)
	default:
		var buffer bytes.Buffer
		_ = format.Node(&buffer, token.NewFileSet(), expr)
		return buffer.String()
	}
}

func gitFiles(root string) ([]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "-z")
	raw, err := command.Output()
	if err != nil {
		return nil, err
	}
	items := strings.Split(string(raw), "\x00")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			result = append(result, filepath.ToSlash(item))
		}
	}
	sort.Strings(result)
	return result, nil
}

func lineCount(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 8*1024*1024)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "\n", " ")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
