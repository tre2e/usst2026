package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
)

type Notifier struct {
	host    string
	port    int
	user    string
	pass    string
	toEmail string
}

func NewNotifier(cfg *Config) *Notifier {
	return &Notifier{
		host:    cfg.SMTPHost,
		port:    cfg.SMTPPort,
		user:    cfg.SMTPUser,
		pass:    cfg.SMTPPass,
		toEmail: cfg.NotifyEmail,
	}
}

func (n *Notifier) SendRollcallAlert(courseName, teacher, status, detail string) error {
	subject := fmt.Sprintf("【签到通知】%s — %s", courseName, status)
	body := fmt.Sprintf(
		"课程: %s\r\n教师: %s\r\n状态: %s\r\n详情: %s\r\n",
		courseName, teacher, status, detail,
	)
	return n.send(subject, body)
}

func (n *Notifier) send(subject, body string) error {
	addr := fmt.Sprintf("%s:%d", n.host, n.port)
	auth := smtp.PlainAuth("", n.user, n.pass, n.host)

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		n.user, n.toEmail, subject, body,
	)

	if n.port == 465 {
		return n.sendTLS(addr, auth, msg)
	}
	return n.sendSTARTTLS(addr, auth, msg)
}

func (n *Notifier) sendTLS(addr string, auth smtp.Auth, msg string) error {
	tlsCfg := &tls.Config{
		ServerName:         n.host,
		InsecureSkipVerify: false,
	}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		log.Printf("TLS连接失败 (%s): %v — 尝试降级到STARTTLS", addr, err)
		return n.sendSTARTTLS(addr, auth, msg)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		return fmt.Errorf("SMTP客户端创建失败: %w", err)
	}
	defer client.Quit()

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP认证失败: %w", err)
	}
	if err := client.Mail(n.user); err != nil {
		return fmt.Errorf("SMTP MAIL失败: %w", err)
	}
	if err := client.Rcpt(n.toEmail); err != nil {
		return fmt.Errorf("SMTP RCPT失败: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA失败: %w", err)
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return fmt.Errorf("SMTP写入失败: %w", err)
	}
	return w.Close()
}

func (n *Notifier) sendSTARTTLS(addr string, auth smtp.Auth, msg string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("TCP连接失败: %w", err)
	}

	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		return fmt.Errorf("SMTP客户端创建失败: %w", err)
	}
	defer client.Quit()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{
			ServerName:         n.host,
			InsecureSkipVerify: false,
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS失败: %w", err)
		}
	}

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP认证失败: %w", err)
	}
	if err := client.Mail(n.user); err != nil {
		return fmt.Errorf("SMTP MAIL失败: %w", err)
	}
	if err := client.Rcpt(n.toEmail); err != nil {
		return fmt.Errorf("SMTP RCPT失败: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA失败: %w", err)
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return fmt.Errorf("SMTP写入失败: %w", err)
	}
	return w.Close()
}

func (n *Notifier) Test() error {
	return n.send("【签到监听服务】测试邮件", "服务已启动，SMTP配置正确。")
}
