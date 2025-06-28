package nga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/imroc/req/v3"
	"github.com/panjf2000/ants/v2"
	"github.com/spf13/cast"
	"gopkg.in/ini.v1"
)

// 这里是配置文件可以改的
var (
	CFGFILE_THREAD_COUNT              = 2
	CFGFILE_PAGE_DOWNLOAD_LIMIT       = 100 //	限制单次下载的页数 #56
	CFGFILE_USE_TITLE_AS_FOLDER_NAME  = false
	CFGFILE_USE_TITLE_AS_MD_FILE_NAME = false
)

var Directory string

// 这里传参可以改
// var ()

// 这里配置文件和传参都没法改
var (
	VERSION  = "1.7.1"      //需要手动改
	BUILD_TS = "1691664141" //无需，GitHub actions会自动填写
	GIT_REF  = ""           //无需，GitHub actions会自动填写
	GIT_HASH = ""           //无需，GitHub actions会自动填写
	DELAY_MS = 330
	mutex    sync.Mutex
)

// Flag 相关
var (
	page_download_limit_triggered = false
)

// ldflags 区域。GitHub Actions 编译时会使用 ldflags 来修改如下值：
var (
	DEBUG_MODE = "1" //GitHub Actions 打包的时候会修改为"0"。本地打包可以 go build -ldflags "-X 'github.com/hld67890/stage1stpost2md/nga.DEBUG_MODE=0'" main.go
	/**
	 * DEBUG_MODE 为true时会:
	 * 启动时禁用自动版本检查
	 */
)

type Goose struct {
	id     string
	idlink string
	num    string
	reason string
}

type Floor struct {
	Lou          int
	Pid          int
	Timestamp    int64
	Username     string
	UserId       int
	Content      string
	JiaGoose     []Goose
	JiaGooseFlag bool
	TotalGoose   string
	Contenthtml  *goquery.Selection
	JiaGoosehtml *goquery.Selection
	Good         int
	Power        int
	Reply        int
	Regtime      string
	Deleted      bool
}
type Floors []Floor
type Tiezi struct {
	Tid             int
	AuthorId        int //这个是用户传入的希望仅下载某用户id的发言贴参数
	Title           string
	TitleFolderSafe string
	Catelogy        string //真不是category？？？
	Username        string
	UserId          int
	WebMaxPage      int
	LocalMaxPage    int
	LocalMaxFloor   int
	FloorCount      int    //包含主楼
	Floors          Floors //主楼为[0]
	Timestamp       int64  //page() fixFloorContent()  中会更新
	Version         string //这个是软件的version
	Assets          map[string]string
}

var responseChannel = make(chan string, 15)

/**
 * @description: 分析floors原始数据并填充进floors里
 * @param {[]byte} resp 接口下来的原始数据
 * @return {*}
 */
func (it *Floors) analyze(postlist *goquery.Selection) {
	// fmt.Println("posts@@", postlist.Text())
	postlist.Children().Filter("div:not(.pl)").Each(func(i int, post *goquery.Selection) {
		// fmt.Println("@@@@@@@@", i, post.Text())
		floor_count := post.Find("td.plc div.pi strong a em").Text()

		var lou int
		if floor_count == "" {
			lou = 0
		} else if floor_count == "顶" {
			//这论坛还能置顶帖子？？？tid=1826103
			floor_count = post.Find("td.plc div.pi strong a").Text()
			lou, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(floor_count, "\n顶 来自 "), "#"))
			lou -= 1
			// fmt.Println("@@@@@@@@", i, lou, floor_count)
		} else {
			lou = cast.ToInt(floor_count) - 1
		}
		// 根据楼数补充Floors
		for len(*it) < lou+1 {
			(*it) = append((*it), Floor{Lou: -1})
		}
		// fmt.Println("@@@@@@@@", i, lou, floor_count)
		curFloor := &(*it)[lou]

		//楼层
		curFloor.Lou = lou

		//PID
		id, exists := post.Attr("id")
		if !exists {
			id = "post_-1"
		}
		id = strings.TrimPrefix(id, "post_")
		curFloor.Pid = cast.ToInt(id)
		// fmt.Println("@@@@@@@@", i, curFloor.Pid)

		//时间戳
		timestamp := strings.TrimPrefix(post.Find("td.plc div.pi div.pti div.authi em").Text(), "发表于 ")
		curFloor.Timestamp, _ = TimeStringToTimestamp(timestamp)
		// fmt.Println("@@@@@@@@", timestamp, curFloor.Timestamp)

		userinfo := post.Find("td.pls div.pls div.pi div.authi a")

		//用户名
		username := userinfo.Text()
		curFloor.Username = username
		// fmt.Println("@@@@@@@@", username)

		//用户id
		idstr, exists := userinfo.Attr("href")
		if !exists {
			idstr = "space-uid-0.html"
		}
		userid, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(idstr, "space-uid-"), ".html"))
		curFloor.UserId = cast.ToInt(userid)
		// fmt.Println("@@@@@@@@", curFloor.UserId)

		//已被删帖
		deleted := post.Find("div.pct div.pcb div.locked")
		if deleted.Length() > 0 {
			curFloor.Deleted = true
		}

		//内容
		content := post.Find("div.pct div.pcb div.pcbs")
		if content.Length() == 0 {
			content = post.Find("div.pct div.pcb div.t_fsz")
		}
		curFloor.Contenthtml = content
		// fmt.Println("@@@@@@@@", lou, curFloor.Contenthtml)

		//加鹅情况
		//需要上面的id
		//如果没有加鹅记录就找不到
		jiaGoose := post.Find("div.pct div.pcb dl#ratelog_" + id)
		// fmt.Println("@@@@@@@@", lou, id, jiaGoose.Length())
		if jiaGoose.Length() != 0 {
			link, _ := jiaGoose.Find("p.ratc a").Attr("href")
			// fmt.Println("@@@@@@@@", lou, link, jiaGoose.Find("p.ratc a").Text())
			link = strings.TrimSuffix(strings.TrimPrefix(link, "\""), "\"")
			resp, err := Client.R().Post("2b/" + link)
			if err != nil {
				log.Println(err.Error())
			} else {
				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					log.Println(err.Error())
				}
				doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(bodyBytes)))
				if err != nil {
					log.Println(err.Error())
					return
				}
				curFloor.JiaGooseFlag = true
				curFloor.JiaGoosehtml = doc.Find("div.floatwrap table.list")

				re := regexp.MustCompile(`战斗力\s*(.*?)\s*鹅`)
				matches := re.FindStringSubmatch(doc.Find("div.pns").Text())
				if len(matches) > 1 {
					curFloor.TotalGoose = matches[1]
				}
				// fmt.Println("@@@@@@@@", doc.Find("div.pns").Text(), "@@@", curFloor.TotalGoose)
			}
		}

		//精华战斗力回帖
		gprElement := post.Find("div.tns table")
		good, _ := strconv.Atoi(strings.TrimSuffix(gprElement.Children().Eq(0).Children().Eq(0).Children().Eq(0).Text(), "精华"))
		power, _ := strconv.Atoi(strings.TrimSuffix(gprElement.Children().Eq(0).Children().Eq(0).Children().Eq(1).Text(), "战斗力"))
		reply, _ := strconv.Atoi(strings.TrimSuffix(gprElement.Children().Eq(0).Children().Eq(0).Children().Eq(2).Text(), "回帖"))
		// fmt.Println("@@@@@@@@", good, power, reply)
		curFloor.Good = good
		curFloor.Power = power
		curFloor.Reply = reply

		//注册时间
		regtime := post.Find("div.i div.cl p").Slice(2, 3)
		curFloor.Regtime = strings.TrimPrefix(regtime.Text(), "注册时间  ")
		// fmt.Println("@@@@@@@@", curFloor.Regtime)
	})

	// 	//点赞数
	// 	value_int, _ = jsonparser.GetInt(value, "vote_good")
	// 	curFloor.LikeNum = cast.ToInt(value_int)

	// 	//下挂comments
	// 	value_byte, dataType, _, _ := jsonparser.Get(value, "comments")
	// 	if dataType != jsonparser.NotExist {
	// 		curFloor.Comments.analyze(value_byte, true)
	// 	}
	// })
}

/**
 * @description: 针对 tiezi 对象获取指定页的信息
 * @param {int} page 指定的页数
 * @return {*}
 */
func (tiezi *Tiezi) page(page int) {
	var resp *req.Response
	var err error
	if tiezi.AuthorId > 0 {
		resp, err = Client.R().SetFormData(map[string]string{
			"page":     cast.ToString(page),
			"tid":      cast.ToString(tiezi.Tid),
			"authorid": cast.ToString(tiezi.AuthorId),
		}).Post("2b/forum.php?mod=viewthread")
	} else {
		resp, err = Client.R().SetFormData(map[string]string{
			"page": cast.ToString(page),
			"tid":  cast.ToString(tiezi.Tid),
		}).Post("2b/forum.php?mod=viewthread")
	}
	if err != nil {
		log.Println(err.Error())
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	// log.Printf("http response %s", string(bodyBytes))

	if err != nil {
		log.Println(err.Error())
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(bodyBytes)))
	if err != nil {
		log.Println(err.Error())
		return
	}

	tiezi.Timestamp = ts()

	// 标题
	value_str := doc.Find("span#thread_subject").Text()
	tiezi.Title = value_str
	tiezi.TitleFolderSafe = ToSaveFilename(tiezi.Title)
	log.Printf("标题 %s", tiezi.Title)

	//分区名
	value := doc.Find("div#pt").Find("a")
	if value.Length() >= 4 {
		group := value.Slice(2, 3).Text()
		category := value.Slice(3, 4).Text()
		tiezi.Catelogy = group + "-" + category
	} else {
		tiezi.Catelogy = "未知分区"
	}
	log.Printf("分区 %s", tiezi.Catelogy)

	//好像用不上
	// //作者
	// value_str, _ = jsonparser.GetString(resp.Bytes(), "tauthor")
	// tiezi.Username = value_str

	// //作者id
	// value_int, _ := jsonparser.GetInt(resp.Bytes(), "tauthorid")
	// tiezi.UserId = cast.ToInt(value_int)

	//总页数
	value_str = doc.Find("div.pgt div.pg span").Text()
	parts := strings.Fields(value_str)
	if len(parts) < 2 {
		tiezi.WebMaxPage = 1
	} else {
		num, _ := strconv.Atoi(parts[len(parts)-2])
		tiezi.WebMaxPage = cast.ToInt(num)
	}
	if CFGFILE_PAGE_DOWNLOAD_LIMIT > 0 && tiezi.WebMaxPage > (tiezi.LocalMaxPage+CFGFILE_PAGE_DOWNLOAD_LIMIT) {
		tiezi.WebMaxPage = tiezi.LocalMaxPage + CFGFILE_PAGE_DOWNLOAD_LIMIT
		page_download_limit_triggered = true
	}
	log.Printf("页数 %d", tiezi.WebMaxPage)

	//楼层数，楼主也算一层。这里只需要保存一次总楼数，s1里面只有第一页会显示总楼数，其他页时候就忽略这一项。不要开多线程，下面的初始化floors数会出问题
	//bug：只看楼主的时候还是会显示所有楼层数量，输出的时候也会后空白楼层（变成去层数的max了，应该没问题了，开多线程也应该可以了）
	// if page == 1 {
	// 	value_str = doc.Find("span.xi1").Slice(1, 2).Text()
	// 	num, _ := strconv.Atoi(value_str)
	// 	tiezi.FloorCount = cast.ToInt(num + 1)
	// 	log.Printf("楼层数 %d", tiezi.FloorCount)
	// }

	// //初始化floors个数
	// if len(tiezi.Floors) == 0 {
	// 	tiezi.Floors = make([]Floor, tiezi.FloorCount)
	// 	for i := range tiezi.Floors {
	// 		tiezi.Floors[i].Lou = -1
	// 	}
	// }

	value_items := doc.Find("div#postlist")
	tiezi.Floors.analyze(value_items)

	tiezi.FloorCount = len(tiezi.Floors)
}

/**
 * @description: 本地未生成过。初始化主楼和第一页
 * @param {int} tid 帖子tid
 * @return {*}
 */
func (tiezi *Tiezi) InitFromWeb(tid int, authorId int) {
	tiezi.init(tid, authorId)
	tiezi.Version = VERSION
	tiezi.Assets = map[string]string{}
	tiezi.LocalMaxPage = 1
	tiezi.LocalMaxFloor = -1
	log.Printf("下载第 %02d 页\n", tiezi.LocalMaxPage)
	tiezi.page(tiezi.LocalMaxPage)
}

/**
 * @description: 本地已经有生成过，现在来根据local信息来追加下载新楼层。
 * @param {int} tid 帖子tid
 * @return {*}
 */
func (tiezi *Tiezi) InitFromLocal(tid int, authorId int) {
	tiezi.init(tid, authorId)
	tiezi.Version = VERSION

	checkFileExistence := func(fileName string) {
		if _, err := os.Stat(fileName); os.IsNotExist(err) {
			log.Fatalln(fileName, "文件丢失，软件将退出。")
		}
	}
	folderName := FindFolderNameByTid(tid, authorId, Directory)
	if folderName == "" {
		log.Fatalln("找不到本地 tid 文件夹，软件将退出。")
	}
	processFileName := fmt.Sprintf("%s/process.ini", folderName)
	checkFileExistence(processFileName)

	assetsFileName := fmt.Sprintf("%s/assets.json", folderName)
	checkFileExistence(assetsFileName)

	jsonBytes, _ := os.ReadFile(assetsFileName)
	err := json.Unmarshal(jsonBytes, &(tiezi.Assets))
	if err != nil {
		log.Fatalln("解析 assets.json 失败:", err.Error())
	}
	cfg, _ := ini.Load(processFileName)
	tiezi.LocalMaxPage = cfg.Section("local").Key("max_page").MustInt(1)
	tiezi.LocalMaxFloor = cfg.Section("local").Key("max_floor").MustInt(-1)
	log.Printf("下载第 %02d 页\n", tiezi.LocalMaxPage)
	tiezi.page(tiezi.LocalMaxPage)

}

/**
 * @description: 初始化 Tiezi。
 * @param {int} tid 帖子tid
 * @return {*}
 */
func (tiezi *Tiezi) init(tid int, authorId int) {
	tiezi.Tid = tid
	tiezi.AuthorId = authorId
}

/**
 * @description: 由传入 Tiezi 对象里根据 pid 查找一 Floor 对象。若没有查到则返回空
 * @param {int} pid 楼层 pid
 * @return {*}
 */
func (tiezi *Tiezi) findFloorByPid(pid int) *Floor {
	for _, v := range tiezi.Floors {
		if v.Pid == pid {
			return &v
		}
	}
	return nil
}

/**
 * @description: 处理帖子正文内容。
 * @description: 不支持表格，太离谱了。论坛的表格里面可以嵌套列表和引用，markdown不支持。表格会丢掉格式把内容下载下来。
 * @description: 二手交易的格式会比较乱，但是因为不常用，不管了。
 * @description: 附件格式上传的图片会有一些冗余信息。非图片附件不会自动下载，只有链接（没人在这上传附件吧）。
 * @description: 重复图片只会下载一次
 * @description: 帖子示例：tid=2244111，样式大全；tid=1826103，置顶帖子
 * @description: tid=2253699，多选，公开投票；tid=2204769，多选，投票后可见；tid=2253581，单选，投票后可见；
 * @description: tid=2249069，被删贴
 * @description: tid=2253696，二手区
 * @param {*goquery.Selection} doc 帖子正文的html对象
 * @param {int} indent 调试用缩进
 * @param {*map[string]string} assets 保存图片资源
 * @param {*Tiezi} tiezi 原帖子指针
 * @param {*Floor} floor 原楼层指针
 * @param {string} newline 是否处于块引用、无序列表、有序列表中，支持嵌套
 * @param {string} listType 当前元素的列表类型，取值为无序列表unorderList，有序列表orderlist，代码块和其他
 * @return {*}
 */
func parseAndListStructured(doc *goquery.Selection, indent int, assets *map[string]string, tiezi *Tiezi, floor *Floor, newline string, listType string) {
	var stopFlag bool = false
	doc.Contents().Each(func(i int, s *goquery.Selection) {
		if stopFlag {
			return
		}

		// 跳过纯文本节点（只包含空白字符的）
		if goquery.NodeName(s) == "#text" {
			if strings.TrimSpace(s.Text()) == "" {
				return
			}
		}

		// // 打印缩进
		// fmt.Print(strings.Repeat("  ", indent))

		// 处理不同类型的节点
		switch goquery.NodeName(s) {
		case "#text":
			//文字
			(*floor).Content += s.Text()
			// fmt.Printf("文本: %q\n", strings.TrimSpace(s.Text()))
		case "blockquote":
			// fmt.Printf("引用块\n")
			//引用块
			(*floor).Content += "> "
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline+"> ", "")
			(*floor).Content += "\n" + newline
		case "br":
			// fmt.Printf("换行\n")
			//换行
			(*floor).Content += "\n" + newline
		case "font":
			// fmt.Printf("字体设置\n")
			//字体大小颜色
			font := s.Get(0)
			Key := ""
			Val := ""
			for _, attr := range font.Attr {
				Key = attr.Key
				Val = attr.Val
				if Val[0] != '"' {
					Val = "\"" + Val + "\""
				}
			}

			var defaultSetting bool = false
			if Key == "face" && Val == `"&amp;quot;"` {
				defaultSetting = true //看起来是没用的字体设置，跳过
			}
			if Key == "face" && Val == `"&quot;"` {
				defaultSetting = true //默认字体颜色
			}
			if Key == "face" && Val == `"&quot"` {
				defaultSetting = true //默认字体颜色
			}
			if Key == "color" && Val == `"#333333"` {
				defaultSetting = true //默认字体颜色
			}
			if Key == "style" && Val == `"color:rgb(51, 51, 51)"` {
				defaultSetting = true //默认字体颜色
			}
			if Key == "color" && Val == `"#4183c4"` {
				defaultSetting = true //默认超链接颜色
			}
			if Key == "style" && Val == `"font-size:16px"` {
				defaultSetting = true //默认字体大小
			}
			// fmt.Printf("@@@@@ %s %s\n", Key, Val)

			if Key != "" && !defaultSetting {
				(*floor).Content += "<font " + Key + "=" + Val + ">"
			}
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")
			if Key != "" && !defaultSetting {
				(*floor).Content += "</font>"
			}
		case "strong":
			// fmt.Printf("加粗\n")
			//加粗
			(*floor).Content += "<strong>"
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")
			(*floor).Content += "</strong>"
		case "img":
			//图片
			//不管图片缩放了，直接贴上来
			//上传的图片的地址在file属性里面，但是有缩放的时候好像会有src属性的占位符
			//表情包的地址在src属性里面
			//用附件方式上传的图片也能解析出来，但是会有无用的额外信息显示
			var source string
			var exists bool
			source, exists = s.Attr("file")
			if !exists {
				source, exists = s.Attr("src")
				if !exists {
					log.Printf("未找到图片链接")
				}
			}
			url := source
			sha := sha256.Sum256([]byte(url))
			shaStr := hex.EncodeToString(sha[:])
			shorted := shaStr[2:8] + url[len(url)-6:]
			//防止文件名太短，包括了上一个目录的"/"
			shorted = strings.ReplaceAll(shorted, "/", "_")
			var fileName string

			mutex.Lock()
			var ok bool
			v, ok := (*assets)[shorted]
			if ok {
				//存在，直接复用
				fileName = v
			} else {
				fileName = cast.ToString(floor.Lou) + "_" + shorted
				(*assets)[shorted] = fileName
			}
			if !ok {
				mutex.Unlock()
				time.Sleep(time.Millisecond * time.Duration(DELAY_MS))
				// log.Println("@@@@ 下载图片:", url, tiezi.GetNeededFolderName(), fileName)
				downloadAssets(url, tiezi.GetNeededFolderName()+`/`+fileName)
				// log.Println("下载图片成功:", fileName)
			} else {
				mutex.Unlock()
			}
			(*floor).Content += `![img](./` + fileName + `)`

			//说明是用户上传的图片，只需要把第一个<img>元素下下来，后边的是鼠标放上去的详情。
			// stopFlag = true

			// fmt.Printf("图片: %q %s\n", source, `![img](./`+fileName+`)`)
		case "a":
			//超链接
			var source string
			source, _ = s.Attr("href")

			// fmt.Printf("链接: %q\n", source)

			(*floor).Content += `[`
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")
			(*floor).Content += `](` + source + `)`
		case "div":
			//div元素，处理居中和右对齐，代码块
			// fmt.Printf("<%s>", goquery.NodeName(s))

			// // 打印属性
			// if attrs := s.Nodes[0].Attr; len(attrs) > 0 {
			// 	fmt.Print(" [")
			// 	for j, attr := range attrs {
			// 		if j > 0 {
			// 			fmt.Print(" ")
			// 		}
			// 		fmt.Printf("%s=%q", attr.Key, attr.Val)
			// 	}
			// 	fmt.Print("]")
			// }
			// fmt.Println()

			//居中&右对齐
			//左对齐也得放进来，会有一个换行的效果
			if val, exists := s.Attr("align"); exists {
				if val != "left" {
					(*floor).Content += "\n" + newline + "<div style=\"text-align: " + val + "\">"
				} else {
					(*floor).Content += "\n" + newline
				}
				parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")
				if val != "left" {
					(*floor).Content += `</div>`
				}
				break
			}

			//代码块
			if val, exists := s.Attr("class"); exists && val == "blockcode" {
				(*floor).Content += "\n" + newline + "```" + "\n" + newline
				parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "blockcode")
				(*floor).Content += "```\n" + newline
				break
			}

			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")

		case "ul":
			//无序列表和有序列表的开始
			listType = "unorderList"
			if _, exists := s.Attr("type"); exists {
				listType = "orderlist"
			}
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, listType)
			(*floor).Content += "\n" + newline
		case "li":
			//无序列表和有序列表的每个条目
			//代码块不需要额外的标记
			if listType == "orderlist" {
				(*floor).Content += strconv.Itoa(i+1) + ". "
			} else if listType == "unorderList" {
				(*floor).Content += "- "
			}
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")
			(*floor).Content += "\n" + newline
		case "label":
			//投票选项
			(*floor).Content += strings.ReplaceAll(s.Text(), "\u00A0", " ") + "\n" + newline
		default:
			//其他元素
			// fmt.Printf("<%s>", goquery.NodeName(s))

			// // 打印属性
			// if attrs := s.Nodes[0].Attr; len(attrs) > 0 {
			// 	fmt.Print(" [")
			// 	for j, attr := range attrs {
			// 		if j > 0 {
			// 			fmt.Print(" ")
			// 		}
			// 		fmt.Printf("%s=%q", attr.Key, attr.Val)
			// 	}
			// 	fmt.Print("]")
			// }
			// fmt.Println()

			// 递归处理子节点
			parseAndListStructured(s, indent+1, assets, tiezi, floor, newline, "")
		}

		//去掉代码块最后的“复制代码”按钮
		if listType == "blockcode" {
			stopFlag = true
		}
		//去掉图片后面的下载附件弹窗
		if goquery.NodeName(doc) == "ignore_js_op" && goquery.NodeName(s) == "img" {
			stopFlag = true
		}
	})
}

/**
 * @description: 处理加鹅信息。
 * @param {*goquery.Selection} doc 加鹅的html对象
 * @param {*Floor} floor 原楼层指针
 * @return {*}
 */
func parseJiaGoose(doc *goquery.Selection, floor *Floor) {
	doc.Find("tr").Each(func(i int, s *goquery.Selection) {
		items := s.Find("td")
		scorehtml := items.Slice(0, 1)
		if scorehtml.Text() == "积分" {
			return
		}
		idhtml := items.Slice(1, 2).Find("a")
		idlink, exists := idhtml.Attr("href")
		reasonhtml := items.Slice(3, 4)
		// fmt.Print("@@", idlink, exists)

		var curGoose Goose
		curGoose.id = idhtml.Text()
		if exists {
			curGoose.idlink = BASE_URL + "/2b/" + strings.TrimSuffix(strings.TrimPrefix(idlink, "\""), "\"")
		}
		curGoose.num = strings.TrimSuffix(strings.TrimPrefix(scorehtml.Text(), "战斗力 "), " 鹅")
		curGoose.reason = reasonhtml.Text()
		(*floor).JiaGoose = append((*floor).JiaGoose, curGoose)
	})
}

/**
 * @description: 由bbcode转md，以及下载图片、转化表情等
 * @param {int} floor_i floor下标
 * @return {*}
 */
func (tiezi *Tiezi) fixContent(floor_i int) {
	parseAndListStructured(tiezi.Floors[floor_i].Contenthtml, 0, &tiezi.Assets, tiezi, &tiezi.Floors[floor_i], "", "")
	if tiezi.Floors[floor_i].JiaGooseFlag {
		parseJiaGoose(tiezi.Floors[floor_i].JiaGoosehtml, &tiezi.Floors[floor_i])
	}
}

/**
 * @description: 对fixContent的包裹。主要是为了并行……
 * @param {int} startFloor_i 从哪一下标开始修。主要是针对追加楼层更新时
 * @return {*}
 */
func (tiezi *Tiezi) fixFloorContent(startFloor_i int) {

	var wg sync.WaitGroup
	p, _ := ants.NewPoolWithFunc(CFGFILE_THREAD_COUNT, func(floor_i interface{}) {
		if tiezi.Floors[cast.ToInt(floor_i)].Lou != -1 {
			responseChannel <- fmt.Sprintf("开始修正第 %02d 楼层", cast.ToInt(floor_i))
			tiezi.fixContent(cast.ToInt(floor_i))
		}
		wg.Done()
	})
	defer p.Release()

	startTime := time.Now()
	for i := startFloor_i; i < len(tiezi.Floors); i++ {
		wg.Add(1)
		_ = p.Invoke(i)
		tiezi.Timestamp = ts()
	}
	wg.Wait()
	log.Println("修正楼层总耗时:", time.Since(startTime).Truncate(time.Second).String())
	// 如果为了调试取消并行的话，上述代码均注释，换成下面的
	// for i := startFloor_i; i < len(tiezi.Floors); i++ {
	// 	log.Printf("开始修正第 %02d 楼层", i)
	// 	tiezi.fixContent(cast.ToInt(i))
	// }

}

/**
 * @description: 写markdown文件
 * @param {int} localMaxFloor 本地已有的楼
 * @return {*}
 */
func (tiezi *Tiezi) genMarkdown(localMaxFloor int) {
	folder := fmt.Sprintf("%s/", tiezi.GetNeededFolderName())
	os.MkdirAll(folder, os.ModePerm)
	//后续判断md文件名。假如 post.md存在则继续沿用，否则根据个性化来设置
	mdFilePath := filepath.Join(folder, "post.md")
	if _, err := os.Stat(mdFilePath); os.IsNotExist(err) {
		//post.md不存在，判断是否需要个性化
		if CFGFILE_USE_TITLE_AS_MD_FILE_NAME {
			mdName := fmt.Sprintf("%s.md", tiezi.TitleFolderSafe)
			mdFilePath = filepath.Join(folder, mdName)
		}
	}

	if _, err := os.Stat(mdFilePath); os.IsNotExist(err) {
		if _, err := os.Create(mdFilePath); err != nil {
			log.Fatalf("创建或打开 .md 文件失败：%v", err)
		}
	}

	f, err := os.OpenFile(mdFilePath, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("创建或打开 .md 文件失败：%v", err)
	}
	defer f.Close()
	for i := localMaxFloor; i < len(tiezi.Floors); i++ {
		floor := &tiezi.Floors[i]
		if floor.Deleted {
			//被抽楼了
			floor.Content = "[提示: 作者被禁止或删除 内容自动屏蔽]"
		}

		if floor.Pid == 0 {
			var authorIdOptText = ""
			if tiezi.AuthorId > 0 {
				authorIdOptText = fmt.Sprintf("-只看 %d", tiezi.AuthorId)
			}
			_, _ = f.WriteString(fmt.Sprintf("### %s%s\n\nMade by ngapost2md (c) ludoux [GitHub Repo](https://github.com/ludoux/ngapost2md)\n\n", tiezi.Title, authorIdOptText))
		}

		isOP := ""
		if floor.UserId == tiezi.Floors[0].UserId && tiezi.AuthorId == 0 {
			isOP = " [楼主]"
		}
		_, _ = f.WriteString(fmt.Sprintf("----\n\n##### <span id=\"pid%d\">%d \\<pid:%d\\> %s by [%s](%s/2b/space-uid-%d.html)%s 精华 %d 战斗力 %d 回帖 %d 注册时间 %s</span>\n%s", floor.Pid, floor.Lou, floor.Pid, ts2t(floor.Timestamp), floor.Username, BASE_URL, floor.UserId, isOP, floor.Good, floor.Power, floor.Reply, floor.Regtime, floor.Content))

		//加鹅信息
		if len(floor.JiaGoose) > 0 {
			_, _ = f.WriteString(fmt.Sprintf("\n| 参与人数 %d | 战斗力 %s  | 理由 |\n| ------------ | ---- | ----------------------- |\n", len(floor.JiaGoose), floor.TotalGoose))
			for _, goose := range floor.JiaGoose {
				_, _ = f.WriteString(fmt.Sprintf("| [%s](%s) | %s | %s |\n", goose.id, goose.idlink, goose.num, goose.reason))
			}
		}

		_, _ = f.WriteString("\n\n")
	}
}

func responseController() {
	for rc := range responseChannel {
		log.Println(rc)
	}
}

// 会首先调用FindFolderNameByTid，确定本地没有相关文件夹再返回指定格式文件名。否则返回本地已有文件名
func (tiezi *Tiezi) GetNeededFolderName() string {
	already := FindFolderNameByTid(tiezi.Tid, tiezi.AuthorId, Directory)
	if already != "" {
		return already
	}
	if CFGFILE_USE_TITLE_AS_FOLDER_NAME {
		if tiezi.AuthorId > 0 {
			return fmt.Sprintf("%s%d(%d)-%s", Directory, tiezi.Tid, tiezi.AuthorId, tiezi.TitleFolderSafe)
		} else {
			return fmt.Sprintf("%s%d-%s", Directory, tiezi.Tid, tiezi.TitleFolderSafe)
		}
	} else {
		if tiezi.AuthorId > 0 {
			return fmt.Sprintf("%s%d(%d)", Directory, tiezi.Tid, tiezi.AuthorId)
		} else {
			return Directory + cast.ToString(tiezi.Tid)
		}
	}
}

func (tiezi *Tiezi) SaveProcessInfo() {
	folder := fmt.Sprintf("%s/", tiezi.GetNeededFolderName())

	fileName := filepath.Join(folder, "process.ini")
	cfg := ini.Empty()
	cfg.NewSection("local")
	cfg.Section("local").NewKey("max_floor", cast.ToString(tiezi.LocalMaxFloor))
	cfg.Section("local").NewKey("max_page", cast.ToString(tiezi.LocalMaxPage))
	cfg.SaveTo(fileName)
}

func (tiezi *Tiezi) SaveAssetsMap() {
	folder := fmt.Sprintf("%s/", tiezi.GetNeededFolderName())

	fileName := filepath.Join(folder, "assets.json")
	result, err := json.Marshal(tiezi.Assets)
	if err != nil {
		log.Fatalln("将附件转化为 Json 格式失败:", err.Error())
	}
	f, _ := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY, 0666)
	_, err = f.Write(result)
	if err != nil {
		log.Fatalln("保存 assets.json 文件失败:", err.Error())
	}
	defer f.Close()
}

func (tiezi *Tiezi) Download() {
	if tiezi.Tid != 0 {
		var wg sync.WaitGroup
		p, _ := ants.NewPoolWithFunc(CFGFILE_THREAD_COUNT, func(page interface{}) {
			time.Sleep(time.Millisecond * time.Duration(DELAY_MS))
			responseChannel <- fmt.Sprintf("下载第 %02d 页", page)
			//1. 并行下载page
			tiezi.page(cast.ToInt(page))
			wg.Done()
		})
		defer p.Release()
		go responseController()

		startTime := time.Now()
		//因为 it.LocalMaxPage 在InitFromxxx的时候已经page过了
		for page := tiezi.LocalMaxPage + 1; page <= tiezi.WebMaxPage; page++ {
			wg.Add(1)
			_ = p.Invoke(page)
		}
		wg.Wait()

		log.Println("下载所有页面总耗时:", time.Since(startTime).Truncate(time.Second).String())
		if page_download_limit_triggered {
			log.Println("单次下载 Page 数已达上限！本次导出完毕后需要多次重新运行才可全部导出此帖内容。")
		}

		//2. 格式化content
		tiezi.fixFloorContent(tiezi.LocalMaxFloor + 1)

		//3. 制作文件
		tiezi.genMarkdown(tiezi.LocalMaxFloor + 1)

		tiezi.LocalMaxPage = tiezi.WebMaxPage

		//因为NGA会抽楼，floorcount不准，只能这样子
		for i := len(tiezi.Floors) - 1; ; i-- {
			floor := &tiezi.Floors[i]
			if floor.Lou > -1 {
				tiezi.LocalMaxFloor = floor.Lou
				break
			}
		}
		// 存储tiezi---暂时注释掉，还是使用存储localmaxpage和maxfloor(SaveProcessInfo)的方法。
		//tiezi.SaveAsFile()

		//存储localmaxpage和maxfloor
		tiezi.SaveProcessInfo()

		//存储assets map
		tiezi.SaveAssetsMap()
		if page_download_limit_triggered {
			log.Println("单次下载 Page 数已达上限！本次导出完毕后需要多次重新运行才可全部导出此帖内容。")
		}
		log.Println("本次任务结束。")
	}
}

func Login(username string, password string) {
	loginURL := "https://stage1st.com/2b/member.php?mod=logging&action=login&loginsubmit=yes&infloat=yes&lssubmit=yes&inajax=1&username=" + username + "&password=" + password

	// 发起登录请求
	resp, err := Client.R().
		Post(loginURL)
	if err != nil {
		log.Fatalf("登录失败: %v", err)
	}
	log.Println("登录成功:", resp)
}
