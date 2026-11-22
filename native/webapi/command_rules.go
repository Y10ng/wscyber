package webapi

import (
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// commandRule 是终端命令高危规则表的一行；Match 收到一个调用的词（已去引号，
// 动态展开部分保留原文），返回 true 即需人工审批。新增规则只加一行。
type commandRule struct {
	Name  string
	Match func(words []string) bool
}

var (
	sqlDestructive = regexp.MustCompile(`(?i)\b(drop\s+(table|database|schema|index|view|user)\b|truncate\s+(table\s+)?[\w"\[]|delete\s+from\b)`)
	blockDevice    = regexp.MustCompile(`^/dev/(sd|hd|vd|xvd|nvme|mmcblk|md|dm-|disk|mapper/|loop)`)
)

var commandRules = []commandRule{
	{"递归/强制删除（rm -r / -f）", func(w []string) bool { return program(w) == "rm" && anyFlag(w[1:], "rRf", "--recursive", "--force") }},
	{"格式化/擦除文件系统（mkfs、wipefs、shred）", func(w []string) bool {
		p := program(w)
		return p == "mkfs" || strings.HasPrefix(p, "mkfs.") || p == "wipefs" || p == "shred" || p == "mkswap"
	}},
	{"dd 写设备（of=/dev/…）", func(w []string) bool {
		if program(w) != "dd" {
			return false
		}
		for _, arg := range w[1:] {
			if strings.HasPrefix(arg, "of=/dev/") {
				return true
			}
		}
		return false
	}},
	{"关机/重启（shutdown、reboot、halt、poweroff、init 0|6、systemctl）", func(w []string) bool {
		switch program(w) {
		case "shutdown", "reboot", "halt", "poweroff":
			return true
		case "init", "telinit":
			return len(w) > 1 && (w[1] == "0" || w[1] == "6")
		case "systemctl":
			for _, arg := range w[1:] {
				switch arg {
				case "reboot", "poweroff", "halt", "kexec", "emergency", "rescue":
					return true
				}
			}
		}
		return false
	}},
	{"数据库破坏性语句（DROP / TRUNCATE / DELETE FROM）", func(w []string) bool { return sqlDestructive.MatchString(strings.Join(w, " ")) }},
	{"curl/wget 破坏性请求（DELETE 方法或破坏性信号）", func(w []string) bool {
		p := program(w)
		if p != "curl" && p != "wget" {
			return false
		}
		for i, arg := range w[1:] {
			upper := strings.ToUpper(arg)
			if upper == "-XDELETE" || upper == "--METHOD=DELETE" || upper == "--REQUEST=DELETE" {
				return true
			}
			if (arg == "-X" || arg == "--request" || arg == "--method") && i+2 < len(w) && strings.EqualFold(w[i+2], "DELETE") {
				return true
			}
			if dangerousText(arg) {
				return true
			}
		}
		return false
	}},
	{"浏览器动作含破坏性信号（playwright 点击/填写/提交/导航/脚本等参数命中）", playwrightDestructive},
	{"爬虫主动交互（katana 无头/混合点击、自动填表或自动登录）", katanaActive},
	{"find 删除（-delete）", func(w []string) bool {
		if program(w) != "find" {
			return false
		}
		for _, arg := range w[1:] {
			if arg == "-delete" {
				return true
			}
		}
		return false
	}},
}

// wrappers 只前置执行另一个命令；跳过它们（及其选项/赋值）找实际程序。
var wrappers = map[string]bool{"sudo": true, "doas": true, "env": true, "nohup": true, "nice": true, "ionice": true, "timeout": true,
	"time": true, "command": true, "exec": true, "builtin": true, "xargs": true, "stdbuf": true, "busybox": true, "setsid": true, "chroot": true}

func program(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return path.Base(words[0])
}

func anyFlag(args []string, short string, long ...string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		for _, name := range long {
			if arg == name {
				return true
			}
		}
		if len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && strings.ContainsAny(arg[1:], short) {
			return true
		}
	}
	return false
}

// commandReviewReason 返回命中的高危规则名；空串表示未命中。
// 解析失败时原生 bash 同样无法执行，因此不要求审批。
func commandReviewReason(command string) string {
	return commandReviewDepth(command, 0)
}

func commandReviewDepth(command string, depth int) string {
	if depth > 3 {
		return "嵌套 shell 层数过多，无法逐条核对"
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return ""
	}
	reason := ""
	syntax.Walk(file, func(node syntax.Node) bool {
		if reason != "" {
			return false
		}
		switch n := node.(type) {
		case *syntax.Redirect:
			if n.Word != nil && n.Op != syntax.RdrIn {
				if text, _ := wordText(n.Word); blockDevice.MatchString(text) {
					reason = "重定向写块设备"
				}
			}
		case *syntax.CallExpr:
			words := make([]string, 0, len(n.Args))
			dynamic := false
			for _, word := range n.Args {
				text, literal := wordText(word)
				words = append(words, text)
				dynamic = dynamic || !literal
			}
			if reason = invocationReason(words, depth); reason == "" && dynamic {
				reason = dynamicReason(n, words)
			}
		}
		return reason == ""
	})
	return reason
}

// invocationReason 依次核对：实际调用（包装命令 sudo/env/xargs 等之后的每个位置都按
// 调用起点再核一次）、find -exec 子命令、sh/bash -c、eval 与 tmux send-keys 携带的命令文本。
func invocationReason(words []string, depth int) string {
	wrapped := false
	for j := range words {
		if j > 0 && !wrapped {
			wrapped = wrappers[path.Base(words[j-1])]
		}
		if j > 0 && !wrapped && words[j-1] != "-exec" && words[j-1] != "-execdir" && words[j-1] != "-ok" && words[j-1] != "-okdir" {
			continue
		}
		if strings.HasPrefix(words[j], "-") || isAssignment(words[j]) || wrappers[path.Base(words[j])] {
			continue
		}
		if reason := callReason(words[j:], depth); reason != "" {
			return reason
		}
	}
	return ""
}

func callReason(call []string, depth int) string {
	for _, rule := range commandRules {
		if rule.Match(call) {
			return rule.Name
		}
	}
	nested := ""
	switch program(call) {
	case "sh", "bash", "zsh", "dash", "ksh", "ash":
		for i, arg := range call[1:] {
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c") && i+2 < len(call) {
				nested = call[i+2]
				break
			}
		}
	case "eval":
		nested = strings.Join(call[1:], " ")
	case "tmux":
		if len(call) > 1 && (call[1] == "send-keys" || call[1] == "send") {
			var text []string
			for i := 2; i < len(call); i++ {
				if call[i] == "-t" {
					i++
					continue
				}
				if !strings.HasPrefix(call[i], "-") && call[i] != "Enter" && call[i] != "C-m" {
					text = append(text, call[i])
				}
			}
			nested = strings.Join(text, " ")
		}
	}
	if nested == "" {
		return ""
	}
	return commandReviewDepth(nested, depth+1)
}

func isAssignment(word string) bool {
	index := strings.IndexByte(word, '=')
	return index > 0 && !strings.ContainsAny(word[:index], "/-.")
}

// dynamicSensitive 是参数含动态展开时无法静态排除高危的程序（及解释执行文本的命令）。
var dynamicSensitive = map[string]bool{"rm": true, "dd": true, "shred": true, "wipefs": true, "mkswap": true, "find": true,
	"curl": true, "wget": true, "mysql": true, "psql": true, "sqlite3": true, "mongo": true, "mongosh": true, "redis-cli": true,
	"systemctl": true, "init": true, "telinit": true, "eval": true, "playwright": true, "katana": true, "sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "ash": true, "tmux": true}

// dynamicReason：命令名本身是动态展开，或敏感程序的参数含动态展开时，静态规则无法核对，按需审批处理。
// （本固定上游版本中外部程序不经 command.before，展开后无法再拦截，所以只能在执行前保守判定。）
func dynamicReason(call *syntax.CallExpr, words []string) string {
	for i, word := range call.Args {
		_, literal := wordText(word)
		text := words[i]
		if isAssignment(text) || strings.HasPrefix(text, "-") && literal || wrappers[path.Base(text)] && literal {
			continue
		}
		if !literal {
			return "命令名为动态展开，无法静态核对"
		}
		if !dynamicSensitive[path.Base(text)] && !strings.HasPrefix(path.Base(text), "mkfs") {
			return ""
		}
		return "高危类程序 " + path.Base(text) + " 的参数含动态展开，无法静态核对"
	}
	return ""
}

// wordText 还原字面词：去掉引号；参数展开、命令替换等动态部分保留原文且 literal=false。
func wordText(word *syntax.Word) (string, bool) {
	var out strings.Builder
	literal := true
	var parts func([]syntax.WordPart)
	parts = func(list []syntax.WordPart) {
		for _, part := range list {
			switch p := part.(type) {
			case *syntax.Lit:
				out.WriteString(p.Value)
			case *syntax.SglQuoted:
				out.WriteString(p.Value)
			case *syntax.DblQuoted:
				parts(p.Parts)
			default:
				literal = false
				var printed strings.Builder
				_ = syntax.NewPrinter().Print(&printed, part)
				out.WriteString(printed.String())
			}
		}
	}
	parts(word.Parts)
	return out.String(), literal
}

// playwrightActions 是会向目标发请求或在页面上触发动作的子命令；只读提取（inner-text、snapshot 等）不在内。
var playwrightActions = map[string]bool{"open": true, "goto": true, "content": true, "screenshot": true, "pdf": true, "network": true,
	"tab-new": true, "click": true, "dblclick": true, "tap": true, "press": true, "type": true, "fill": true, "select-option": true,
	"check": true, "uncheck": true, "dispatch-event": true, "autofill": true, "set-input-files": true, "upload": true,
	"evaluate": true, "set-extra-headers": true, "template": true}

// playwrightDestructive：动作类子命令的任一参数（选择器、URL、填入值、脚本、头）命中破坏性信号。
// 纯导航/截图/提取（参数无破坏性信号）不审批。
func playwrightDestructive(w []string) bool {
	if program(w) != "playwright" {
		return false
	}
	args := w[1:]
	index := playwrightSubcommandIndex(args)
	if index < 0 || !playwrightActions[args[index]] {
		return false
	}
	for _, arg := range args[index+1:] {
		if dangerousText(arg) {
			return true
		}
	}
	return false
}

// katanaActive：无头（-headless 会点击页面按钮含提交按钮）、混合（-hybrid 点击 onclick 链接）、
// 自动填表（-aff，会提交表单）、自动登录（-al）。普通只读爬取不审批。
func katanaActive(w []string) bool {
	if program(w) != "katana" {
		return false
	}
	for _, arg := range w[1:] {
		name, value, ok := katanaFlag(arg)
		if !ok || value == "false" {
			continue
		}
		switch name {
		case "headless", "hl", "hybrid", "hh", "automatic-form-fill", "aff", "auto-login", "al":
			return true
		}
	}
	return false
}
