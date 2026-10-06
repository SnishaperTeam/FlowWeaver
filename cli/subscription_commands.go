//go:build headless

package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"snishaper/common"
	"snishaper/pkg/subscription"
)

// subscriptionStore opens the same store the app and core use, so CLI changes
// take effect without a separate import step.
func subscriptionStore() *subscription.Store {
	return subscription.NewStore(common.ConfigSubscriptionPath(execDir()), nil)
}

func formatQuota(info subscription.UserInfo) string {
	if info.Total <= 0 {
		if info.Used() > 0 {
			return "已用 " + formatBytesCLI(info.Used())
		}
		return "无流量信息"
	}

	left := info.Left()
	bar := usageBar(info.Used(), info.Total, 20)
	expire := "长期有效"
	if !info.ExpireDate().IsZero() {
		expire = info.ExpireDate().Format("2006-01-02")
	}
	return fmt.Sprintf("%s  %s / %s (%.0f%%)  到期 %s",
		bar, formatBytesCLI(left), formatBytesCLI(info.Total),
		float64(info.Used())/float64(info.Total)*100, expire)
}

func formatBytesCLI(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

func usageBar(used, total int64, width int) string {
	if total <= 0 {
		return ""
	}
	pct := float64(used) / float64(total)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func opSubscriptionCommand(args []string, out cmdOut) int {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "list", "ls":
		return subscriptionList(out)
	case "add":
		return subscriptionAdd(args[1:], out)
	case "delete", "rm":
		return subscriptionDelete(args[1:], out)
	case "update", "upgrade":
		return subscriptionUpdate(args[1:], out)
	case "use", "activate":
		return subscriptionUse(args[1:], out)
	case "current":
		return subscriptionCurrent(out)
	case "nodes":
		return subscriptionNodes(args[1:], out)
	case "select":
		return subscriptionSelect(args[1:], out)
	case "test":
		return subscriptionTest(args[1:], out)
	default:
		out("用法: sub list|add <名称> <地址>|delete <id>|update [id]|use <id>|current|nodes [id]|select <组> <节点>|test [id]")
		return 2
	}
}

func subscriptionList(out cmdOut) int {
	store := subscriptionStore()
	active := store.ActiveID()

	out("订阅列表:")
	for _, e := range store.List() {
		marker := "  "
		if e.ID == active {
			marker = "* "
		}
		line := fmt.Sprintf("%s%s", marker, e.Name)
		if e.Builtin {
			line += "  [内置规则，无节点]"
		} else {
			line += fmt.Sprintf("  %d 节点", e.NodeCount)
		}
		if e.Error != "" {
			line += "  错误: " + e.Error
		}
		out("  " + line)
		if !e.Builtin {
			out("      " + formatQuota(e.UserInfo))
		}
	}
	return 0
}

func subscriptionAdd(args []string, out cmdOut) int {
	if len(args) == 0 {
		out("用法: sub add <订阅地址> [名称]")
		return 2
	}

	url := args[0]
	name := ""
	if len(args) > 1 {
		name = strings.Join(args[1:], " ")
	}

	out("正在导入 " + url + " ...")
	entry, err := subscriptionStore().Add(name, url)
	if err != nil {
		out("导入失败: " + err.Error())
		return 1
	}

	out("导入成功: " + entry.Name)
	out("  节点数: " + fmt.Sprint(entry.NodeCount))
	out("  代理组: " + fmt.Sprint(entry.RuleCount))
	out("  " + formatQuota(entry.UserInfo))
	out("  查看节点: snishaper sub nodes " + entry.Name)
	return 0
}

func subscriptionDelete(args []string, out cmdOut) int {
	if len(args) == 0 {
		out("用法: sub delete <订阅名或ID>")
		return 2
	}

	store := subscriptionStore()
	id := resolveSubscriptionID(store, args[0])
	if id == "" {
		out("未找到订阅: " + args[0])
		return 1
	}
	if err := store.Delete(id); err != nil {
		out("删除失败: " + err.Error())
		return 1
	}
	out("已删除订阅，当前激活: " + store.ActiveID())
	return 0
}

func subscriptionUpdate(args []string, out cmdOut) int {
	store := subscriptionStore()

	var target *subscription.Entry
	if len(args) > 0 {
		if id := resolveSubscriptionID(store, args[0]); id != "" {
			target = store.Get(id)
		}
	} else {
		for _, e := range store.List() {
			if e.ID == store.ActiveID() && !e.Builtin {
				target = e
				break
			}
		}
	}

	if target == nil {
		out("未找到可更新的订阅")
		return 1
	}
	if target.Builtin {
		out("内置规则订阅无需更新")
		return 0
	}

	out("正在更新 " + target.Name + " ...")
	entry, err := store.Update(target.ID)
	if err != nil {
		out("更新失败: " + err.Error())
		return 1
	}
	out("更新成功: " + entry.Name + "  " + fmt.Sprint(entry.NodeCount) + " 节点")
	out("  " + formatQuota(entry.UserInfo))
	return 0
}

func subscriptionUse(args []string, out cmdOut) int {
	if len(args) == 0 {
		out("用法: sub use <订阅名或ID>")
		return 2
	}

	store := subscriptionStore()
	id := resolveSubscriptionID(store, args[0])
	if id == "" {
		out("未找到订阅: " + args[0])
		return 1
	}
	if err := store.SetActive(id); err != nil {
		out("启用失败: " + err.Error())
		return 1
	}

	entry := store.Get(id)
	out("已启用: " + entry.Name)
	if !entry.Builtin && len(entry.Groups) > 0 {
		out("  可用代理组: " + strings.Join(groupNames(entry.Groups), ", "))
		out("  选择节点: snishaper sub select <组> <节点>")
	}
	return 0
}

func subscriptionCurrent(out cmdOut) int {
	store := subscriptionStore()
	entry := store.Get(store.ActiveID())
	if entry == nil {
		out("未找到当前订阅")
		return 1
	}

	out("当前订阅: " + entry.Name)
	if entry.Builtin {
		out("  内置规则，不提供节点选择")
		return 0
	}
	out("  " + formatQuota(entry.UserInfo))
	for _, g := range entry.Groups {
		selected := entry.Selection[g.Name]
		if selected == "" {
			selected = "未选择"
		}
		out(fmt.Sprintf("  组 %s -> %s", g.Name, selected))
	}
	return 0
}

func subscriptionNodes(args []string, out cmdOut) int {
	store := subscriptionStore()

	var target *subscription.Entry
	if len(args) > 0 {
		if id := resolveSubscriptionID(store, args[0]); id != "" {
			target = store.Get(id)
		}
	} else {
		target = store.Get(store.ActiveID())
	}

	if target == nil {
		out("未找到订阅")
		return 1
	}
	if target.Builtin {
		out("内置规则订阅没有节点")
		return 0
	}

	// Group the nodes by the groups that reference them so the output matches
	// how the user picks a node.
	listed := make(map[string]bool)
	for _, g := range target.Groups {
		out("[" + g.Name + "]")
		if target.Selection[g.Name] != "" {
			out("  当前: " + target.Selection[g.Name])
		}
		for _, member := range g.Members {
			node := findNodeByName(target.Nodes, member)
			if node == nil {
				continue
			}
			listed[member] = true
			marker := "  "
			if target.Selection[g.Name] == member {
				marker = "* "
			}
			line := fmt.Sprintf("%s%-32s %-10s %s:%d", marker, truncate(node.Name, 32), node.Type, node.Server, node.Port)
			if !node.Supported() {
				line += "  [暂不支持]"
			} else if node.UDPSupported() {
				line += "  [TCP+UDP]"
			} else {
				line += "  [仅TCP]"
			}
			out(line)
		}
		out("")
	}

	var orphans []string
	for _, n := range target.Nodes {
		if !listed[n.Name] {
			orphans = append(orphans, n.Name)
		}
	}
	if len(orphans) > 0 {
		out("[未分组的节点]")
		for _, name := range orphans {
			out("  " + name)
		}
		out("")
	}

	out("选择节点: snishaper sub select <组> <节点名>")
	return 0
}

func subscriptionSelect(args []string, out cmdOut) int {
	if len(args) < 2 {
		out("用法: sub select <组> <节点名>")
		out("可用组与节点: snishaper sub nodes")
		return 2
	}

	group := args[0]
	node := strings.Join(args[1:], " ")

	store := subscriptionStore()
	entry := store.Get(store.ActiveID())
	if entry == nil || entry.Builtin {
		out("当前不是可选择节点的订阅")
		return 1
	}
	if findNodeByName(entry.Nodes, node) == nil {
		out("未找到节点: " + node)
		return 1
	}

	if err := store.SelectNode(entry.ID, group, node); err != nil {
		out("选择失败: " + err.Error())
		return 1
	}
	out(fmt.Sprintf("组 %s 已切换到 %s", group, node))
	out("提示: 重新启动服务后生效: snishaper stop && snishaper start")
	return 0
}

func subscriptionTest(args []string, out cmdOut) int {
	store := subscriptionStore()

	var target *subscription.Entry
	if len(args) > 0 {
		if id := resolveSubscriptionID(store, args[0]); id != "" {
			target = store.Get(id)
		}
	} else {
		target = store.Get(store.ActiveID())
	}
	if target == nil || target.Builtin {
		out("内置规则订阅没有可测试的节点")
		return 1
	}

	out("正在测试 " + target.Name + " 的节点 ...")
	ok, fail := 0, 0
	for _, n := range target.Nodes {
		if !n.Supported() {
			out(fmt.Sprintf("  %-32s %-10s 暂不支持", truncate(n.Name, 32), n.Type))
			continue
		}
		_, err := n.Dial("www.gstatic.com", 443)
		if err != nil {
			fail++
			out(fmt.Sprintf("  %-32s %-10s 失败: %v", truncate(n.Name, 32), n.Type, err))
			continue
		}
		ok++
		out(fmt.Sprintf("  %-32s %-10s 可用", truncate(n.Name, 32), n.Type))
	}
	out(fmt.Sprintf("完成: %d 可用, %d 失败", ok, fail))
	return 0
}

func resolveSubscriptionID(store *subscription.Store, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	for _, e := range store.List() {
		if e.ID == query || e.Name == query {
			return e.ID
		}
	}
	// Allow a unique prefix of the name for convenience.
	var matched string
	count := 0
	for _, e := range store.List() {
		if strings.HasPrefix(e.Name, query) {
			matched = e.ID
			count++
		}
	}
	if count == 1 {
		return matched
	}
	return ""
}

func findNodeByName(nodes []subscription.Node, name string) *subscription.Node {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

func groupNames(groups []subscription.Group) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// subscriptionStorePath exposes the resolved file for diagnostics.
func subscriptionStorePath() string {
	return filepath.Clean(common.ConfigSubscriptionPath(execDir()))
}
