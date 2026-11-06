// vaulttool creates the offline credential file without writing plaintext input files.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
	"servicemonitoragent/internal/credentials"
)

type server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

func hidden(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	v, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(v), err
}
func main() {
	id := flag.String("id", "server-1", "服务器 ID")
	name := flag.String("name", "", "显示名称")
	host := flag.String("host", "", "服务器地址")
	port := flag.Int("port", 22, "SSH 端口")
	user := flag.String("user", "", "SSH 用户")
	out := flag.String("out", "credentials.enc", "加密文件路径")
	flag.Parse()
	if *id == "" || *name == "" || *host == "" || *user == "" || *port < 1 || *port > 65535 {
		fmt.Fprintln(os.Stderr, "需要 -id -name -host -user，以及有效的 -port")
		os.Exit(2)
	}
	password, err := hidden("SSH 密码: ")
	if err != nil {
		fatal(err)
	}
	if password == "" {
		fatal(fmt.Errorf("SSH 密码不能为空"))
	}
	passphrase, err := hidden("管理员解锁口令（至少 12 字符）: ")
	if err != nil {
		fatal(err)
	}
	confirm, err := hidden("再次输入管理员口令: ")
	if err != nil {
		fatal(err)
	}
	if passphrase != confirm || len([]rune(passphrase)) < 12 {
		fatal(fmt.Errorf("管理员口令不一致或少于 12 个字符"))
	}
	data, err := json.Marshal([]server{{ID: strings.TrimSpace(*id), Name: *name, Host: *host, Port: *port, User: *user, Password: password}})
	if err != nil {
		fatal(err)
	}
	if _, err = os.Stat(*out); err == nil {
		fatal(fmt.Errorf("输出文件已存在：%s", *out))
	}
	if err = credentials.Save(*out, data, passphrase); err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stdout, "已创建加密凭据文件。管理员口令请独立保管。")
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
