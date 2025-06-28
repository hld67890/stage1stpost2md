package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hld67890/stage1stpost2md/config"
	"github.com/hld67890/stage1stpost2md/nga"
	"github.com/imroc/req/v3"
	"github.com/jessevdk/go-flags"
	"github.com/spf13/cast"
)

type Option struct {
	AuthorId      int    `long:"authorid" default:"0" description:"只下载此 authorid 的发言层"`
	Version       bool   `short:"v" long:"version" description:"显示版本信息并退出"`
	Help          bool   `short:"h" long:"help" description:"显示此帮助信息并退出"`
	GenConfigFile bool   `long:"gen-config-file" description:"生成默认配置文件于 config.ini 并退出"`
	Update        bool   `short:"u" long:"update" description:"检查最新版本"`
	Directory     string `short:"d" long:"dir" description:"导出位置"`
	Listupdate    string `short:"l" long:"listupdate" description:"更新txt文件中所有路径的帖子"`
}

// 检查更新，解析json数据
type Repo struct {
	Tag_name string `json:"tag_name"` // 最新版本号
	Body     string `json:"body"`     // 更新信息为markdown格式
}

func parseNumbersWithRegex(s string) (int, int, error) {
	// 匹配 "整数-" 或 "整数(整数)-"
	re := regexp.MustCompile(`^(\d+)(?:\((\d+)\))?-.*`)
	match := re.FindStringSubmatch(s)
	if match == nil {
		return 0, 0, fmt.Errorf("格式错误")
	}

	// 解析第一个数字
	num1, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, fmt.Errorf("第一个数字无效")
	}

	// 如果有第二个数字（括号内的部分）
	if match[2] != "" {
		num2, err := strconv.Atoi(match[2])
		if err != nil {
			return 0, 0, fmt.Errorf("第二个数字无效")
		}
		return num1, num2, nil
	}

	return num1, 0, nil
}

func checkUpdate() {
	resp, _ := req.C().R().Get("https://api.github.com/repos/ludoux/ngapost2md/releases/latest")

	// 读取最新版本号
	var repo Repo
	err := json.Unmarshal([]byte(resp.String()), &repo)
	if err != nil {
		fmt.Println("解析json数据失败:", err)
	}

	// 输出信息
	log.Printf("目前版本: %s 最新版本: %s", nga.VERSION, repo.Tag_name)
	log.Fatalln("请去 GitHub Releases 页面下载最新版本。软件即将退出……")

}

func downloadTiezi(opts *Option, tie *nga.Tiezi, tid int, authorid int) {
	if opts.Directory != "" {
		nga.Directory = opts.Directory
	} else {
		nga.Directory = "./"
	}
	log.Printf("(%s)", nga.Directory)

	path := nga.FindFolderNameByTid(tid, authorid, opts.Directory)
	if path != "" {
		log.Printf("本地存在此 tid (%s) 文件夹，追加最新更改。", path)
		tie.InitFromLocal(tid, authorid)

	} else {
		tie.InitFromWeb(tid, authorid)
	}

	tie.Download()
}

func main() {
	var opts Option
	parser := flags.NewParser(&opts, flags.Default & ^flags.HelpFlag)
	//args为剩余未解析的（比如tid）
	args, err := parser.Parse()
	if err != nil {
		log.Fatalln("参数解析出现问题:", err.Error())
	}

	if opts.Version {
		fmt.Println("ngapost2md", nga.VERSION)
		fmt.Println("Build_Time:", nga.BUILD_TS, time.Unix(cast.ToInt64(nga.BUILD_TS), 0).Local().Format("2006-01-02T15:04:05Z07:00"))
		fmt.Println("Git_Ref:", nga.GIT_REF)
		fmt.Println("Git_Hash:", nga.GIT_HASH)
		os.Exit(0)
	} else if opts.GenConfigFile {
		err := config.SaveDefaultConfigFile()
		if err != nil {
			log.Fatalln(err.Error())
		}
		log.Println("导出默认配置文件 config.ini 成功。")
		os.Exit(0)
	} else if opts.Help {
		fmt.Println("使用: ngapost2md tid [--authorid aid]")
		fmt.Println("选项与参数说明: ")
		fmt.Println("tid: 待下载的帖子 tid 号")
		fmt.Println("aid: 只看某用户 id 发言层，需配合 --authorid 参数")
		fmt.Println("")
		fmt.Println("ngapost2md -v, --version    ", parser.FindOptionByLongName("version").Description)
		fmt.Println("ngapost2md -h, --help       ", parser.FindOptionByLongName("help").Description)
		fmt.Println("ngapost2md -u, --update     ", parser.FindOptionByLongName("update").Description)
		fmt.Println("ngapost2md -l, --listupdate     ", parser.FindOptionByLongName("listupdate").Description)
		fmt.Println("ngapost2md --gen-config-file", parser.FindOptionByLongName("gen-config-file").Description)
		fmt.Println("ngapost2md -d, --dir", parser.FindOptionByLongName("dir").Description)
		os.Exit(0)
	} else if opts.Update {
		checkUpdate()
	}

	//args check all passed

	fmt.Printf("ngapost2md (c) ludoux [ GitHub: https://github.com/ludoux/ngapost2md/tree/neo ]\nVersion: %s     %s\n", nga.VERSION, time.Unix(cast.ToInt64(nga.BUILD_TS), 0).Local().Format("2006-01-02T15:04:05Z07:00"))
	if nga.DEBUG_MODE == "1" {
		fmt.Println("==debug mode===")
		fmt.Println("***DEBUG MODE ON***")
		fmt.Printf("opts: %+v ; args: %v\n", opts, args)
		fmt.Println("==debug mode===")
	}

	// 检查并按需更新配置文件
	cfg, err := config.GetConfigAutoUpdate()
	if err != nil {
		log.Fatalln(err.Error())
	}

	//获取账号
	var username = cfg.Section("network").Key("username").String()
	var password = cfg.Section("network").Key("password").String()

	nga.BASE_URL = cfg.Section("network").Key("base_url").String()
	nga.UA = cfg.Section("network").Key("ua").String()

	//核心配置项未更改，拒绝执行
	var loginflag bool = true
	if username == "" || strings.Contains(username, "MODIFY_ME") {
		loginflag = false
	}
	if password == "" || strings.Contains(password, "MODIFY_ME") {
		loginflag = false
	}

	//默认线程数为2,仅支持1~3
	nga.CFGFILE_THREAD_COUNT = cfg.Section("network").Key("thread").InInt(2, []int{1, 2, 3})
	nga.CFGFILE_PAGE_DOWNLOAD_LIMIT = cfg.Section("network").Key("page_download_limit").RangeInt(100, -1, 100)
	nga.CFGFILE_USE_TITLE_AS_FOLDER_NAME = cfg.Section("post").Key("use_title_as_folder_name").MustBool()
	nga.CFGFILE_USE_TITLE_AS_MD_FILE_NAME = cfg.Section("post").Key("use_title_as_md_file_name").MustBool()
	nga.Client = nga.NewNgaClient()

	//登录
	if loginflag {
		nga.Login(username, password)
	} else {
		log.Printf("以匿名状态爬取帖子")
	}

	var tid int
	var authorid int

	if opts.Listupdate != "" {

		file, err := os.Open(opts.Listupdate)
		if err != nil {
			fmt.Printf("打开文件失败: %v\n", err)
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			path := scanner.Text()
			if path == "" {
				continue
			}

			// 规范化路径（清理多余的 `/` 或 `\`）
			cleanPath := filepath.Clean(path)

			if len(cleanPath) > 0 && cleanPath[len(cleanPath)-1] == '*' {
				cleanPath = strings.TrimSuffix(cleanPath, "*")
				// 读取该路径下的所有文件和目录
				entries, err := os.ReadDir(cleanPath)
				if err != nil {
					fmt.Printf("无法读取目录 %s: %v\n", cleanPath, err)
					continue
				}

				// 遍历并筛选出子目录
				for _, entry := range entries {
					if entry.IsDir() { // 检查是否是目录
						opts.Directory = cleanPath
						tid, authorid, err = parseNumbersWithRegex(entry.Name())
						if err != nil {
							fmt.Printf("无法读取目录 %s: %v\n", cleanPath, err)
						} else {
							tie := nga.Tiezi{}
							downloadTiezi(&opts, &tie, tid, authorid)
						}
					}
				}
			} else {
				opts.Directory = filepath.Dir(cleanPath) + `\`
				lastDir := filepath.Base(cleanPath)
				tid, authorid, err = parseNumbersWithRegex(lastDir)
				if err != nil {
					fmt.Printf("无法读取目录 %s: %v\n", cleanPath, err)
				} else {
					tie := nga.Tiezi{}
					downloadTiezi(&opts, &tie, tid, authorid)
				}
			}

		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("读取文件失败: %v\n", err)
			return
		}

	} else {
		if len(args) != 1 {
			log.Fatalln("未传入 tid 或格式错误")
		} else {
			tid, err = cast.ToIntE(args[0])
			if err != nil {
				log.Fatalln("tid", args[0], "无法转为数字:", err.Error())
			}
		}
		tie := nga.Tiezi{}
		downloadTiezi(&opts, &tie, tid, opts.AuthorId)
	}
}
