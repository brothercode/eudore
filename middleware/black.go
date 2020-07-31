package middleware

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/eudore/eudore"
)

// BlackStaticHTML 定义黑名单后台html文件位置，默认是当前文件名后缀改为html。
var BlackStaticHTML string

// 获取文件定义位置，静态ui文件在同目录。
func init() {
	_, file, _, ok := runtime.Caller(0)
	if ok {
		BlackStaticHTML = file[:len(file)-2] + "html"
	}
}

// Black 定义黑名单中间件后台。
type black struct {
	White *blackNode
	Black *blackNode
}

// newBlack 函数创建一个黑名单后台。
func newBlack() *black {
	return &black{
		White: new(blackNode),
		Black: new(blackNode),
	}
}

// NewBlackFunc 函数创建一个黑名单处理函数，传入map[string]bool类型参数记录初始化使用的黑/白名单，白名单值为true/黑名单值为false。
func NewBlackFunc(data map[string]bool, router eudore.Router) eudore.HandlerFunc {
	b := newBlack()
	for k, v := range data {
		if v {
			b.InsertWhite(k)
		} else {
			b.InsertBlack(k)
		}
	}
	if router != nil {
		b.InjectRoutes(router)
	}
	return b.HandleHTTP
}

// InjectRoutes 方法将黑名单后台管理功能注入到路由器中。
func (b *black) InjectRoutes(router eudore.Router) {
	router.AnyFunc("/black/ui", HandlerAdmin)
	router.GetFunc("/black/data", b.data)
	router.PutFunc("/black/white/:ip", b.putIP)
	router.PutFunc("/black/black/:ip black=black", b.putIP)
	router.DeleteFunc("/black/white/:ip", b.deleteIP)
	router.DeleteFunc("/black/black/:ip black=black", b.deleteIP)
}

func (b *black) data(ctx eudore.Context) interface{} {
	ctx.SetHeader("X-Eudore-Admin", "black")
	return map[string]interface{}{
		"white": b.White.List(nil, 0, 32),
		"black": b.Black.List(nil, 0, 32),
	}
}

func (b *black) putIP(ctx eudore.Context) {
	ip := fmt.Sprintf("%s/%s", ctx.GetParam("ip"), eudore.GetString(ctx.GetQuery("mask"), "32"))
	ctx.Infof("%s insert %s ip: %s", ctx.RealIP(), eudore.GetString(ctx.GetQuery("black"), "white"), ip)
	if ctx.GetParam("black") != "" {
		b.InsertBlack(ip)
	} else {
		b.InsertWhite(ip)
	}
}

func (b *black) deleteIP(ctx eudore.Context) {
	ip := fmt.Sprintf("%s/%s", ctx.GetParam("ip"), eudore.GetString(ctx.GetQuery("mask"), "32"))
	ctx.Infof("%s delete %s ip: %s", ctx.RealIP(), eudore.GetString(ctx.GetQuery("black"), "white"), ip)
	if ctx.GetParam("black") != "" {
		b.DeleteBlack(ip)
	} else {
		b.DeleteWhite(ip)
	}
}

// HandleHTTP 方法定义黑名单后台的中间件处理函数。
func (b *black) HandleHTTP(ctx eudore.Context) {
	ip := ip2int(ctx.RealIP())
	if b.White.Look(ip) {
		return
	}
	if b.Black.Look(ip) {
		ctx.WriteHeader(403)
		ctx.WriteString("black list deny your ip " + ctx.RealIP())
		ctx.End()
	}
}

// InsertWhite 方法新增一个白名单ip或ip段。
func (b *black) InsertWhite(ip string) {
	b.White.Insert(ip)
}

// InsertBlack 方法新增一个黑名单ip或ip段。
func (b *black) InsertBlack(ip string) {
	b.Black.Insert(ip)
}

// DeleteWhite 方法删除一个白名单ip或ip段。
func (b *black) DeleteWhite(ip string) {
	b.White.Delete(ip)
}

// DeleteBlack 方法删除一个黑名单ip或ip段。
func (b *black) DeleteBlack(ip string) {
	b.Black.Delete(ip)
}

// blackNode 定义黑名单存储树节点。
type blackNode struct {
	Left  *blackNode
	Right *blackNode
	Data  bool
	Count uint64
}

// blackInfo 定义黑名单规则信息。
type blackInfo struct {
	Addr  string `alias:"addr" json:"addr"`
	Mask  uint64 `alias:"mask" json:"mask"`
	Count uint64 `alias:"count" json:"count"`
}

// Insert 方法给黑名单节点新增一个ip或ip段。
func (node *blackNode) Insert(ip string) {
	val, bit := ip2intbit(ip)
	for num := uint(31); bit > 0; bit-- {
		if val>>num&1 == 0 {
			if node.Left == nil {
				node.Left = new(blackNode)
			}
			node = node.Left
		} else {
			if node.Right == nil {
				node.Right = new(blackNode)
			}
			node = node.Right
		}
		num--
	}
	node.Data = true
}

// Delete 方法给黑名单节点删除一个ip或ip段。
func (node *blackNode) Delete(ip string) {
	var lastnode *blackNode
	var lastflag bool
	rootnode := node
	val, bit := ip2intbit(ip)
	for num := uint(31); bit > 0; bit-- {
		if val>>num&1 == 0 {
			if node.Left == nil {
				return
			}
			if node.Right != nil || node.Data {
				lastnode = node
				lastflag = true
			}
			node = node.Left
		} else {
			if node.Right == nil {
				return
			}
			if node.Left != nil {
				lastnode = node
				lastflag = false
			}
			node = node.Right
		}
		num--
	}
	if lastnode != nil {
		if lastflag {
			lastnode.Left = nil
		} else {
			lastnode.Right = nil
		}
	} else {
		*rootnode = blackNode{}
	}
}

// Look 方法匹配ip是否在黑名单节点，命中则节点计数加一。
func (node *blackNode) Look(ip uint64) bool {
	for num := uint(32); num > 0; num-- {
		if node.Data {
			node.Count++
			return true
		}
		if ip>>(num-1)&1 == 0 {
			if node.Left == nil {
				return false
			}
			node = node.Left
		} else {
			if node.Right == nil {
				return false
			}
			node = node.Right
		}
	}
	node.Count++
	return true
}

// List 方法递归获取全部黑名单规则信息。
func (node *blackNode) List(data []blackInfo, prefix, bit uint64) []blackInfo {
	if node.Data {
		data = append(data, blackInfo{
			Addr:  int2ip(prefix << bit),
			Mask:  32 - bit,
			Count: node.Count,
		})
	}
	if node.Left != nil {
		data = node.Left.List(data, prefix<<1, bit-1)
	}
	if node.Right != nil {
		data = node.Right.List(data, prefix<<1|1, bit-1)
	}
	return data
}

func ip2intbit(ip string) (uint64, uint) {
	bit := 32
	pos := strings.Index(ip, "/")
	if pos != -1 {
		bit, _ = strconv.Atoi(ip[pos+1:])
		ip = ip[:pos]
	}
	return ip2int(ip), uint(bit)
}

func ip2int(ip string) uint64 {
	bits := strings.Split(ip, ".")
	b0, _ := strconv.Atoi(bits[0])
	b1, _ := strconv.Atoi(bits[1])
	b2, _ := strconv.Atoi(bits[2])
	b3, _ := strconv.Atoi(bits[3])

	var sum uint64
	sum += uint64(b0) << 24
	sum += uint64(b1) << 16
	sum += uint64(b2) << 8
	sum += uint64(b3)
	return sum
}

func int2ip(ip uint64) string {
	var bytes [4]uint64
	bytes[0] = ip & 0xFF
	bytes[1] = (ip >> 8) & 0xFF
	bytes[2] = (ip >> 16) & 0xFF
	bytes[3] = (ip >> 24) & 0xFF
	return fmt.Sprintf("%d.%d.%d.%d", bytes[3], bytes[2], bytes[1], bytes[0])
}
