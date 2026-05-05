package service

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"license-server/internal/config"
	"net/smtp"
)

// EmailService 邮件服务
type EmailService struct {
	host     string
	port     int
	username string
	password string
	from     string
}

// NewEmailService 创建邮件服务
func NewEmailService() *EmailService {
	cfg := config.Get()
	return &EmailService{
		host:     cfg.Email.SMTPHost,
		port:     cfg.Email.SMTPPort,
		username: cfg.Email.Username,
		password: cfg.Email.Password,
		from:     cfg.Email.From,
	}
}

// SendEmail 发送邮件
func (s *EmailService) SendEmail(to, subject, body string) error {
	if s.host == "" {
		return fmt.Errorf("邮件服务未配置")
	}

	auth := smtp.PlainAuth("", s.username, s.password, s.host)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	// 支持 TLS
	if s.port == 465 {
		return s.sendEmailTLS(to, subject, body)
	}

	return smtp.SendMail(addr, auth, s.from, []string{to}, []byte(msg))
}

// sendEmailTLS 通过 TLS 发送邮件
func (s *EmailService) sendEmailTLS(to, subject, body string) error {
	tlsConfig := &tls.Config{
		ServerName: s.host,
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer client.Close()

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	if err = client.Auth(auth); err != nil {
		return err
	}

	if err = client.Mail(s.from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.from, to, subject, body)

	_, err = w.Write([]byte(msg))
	if err != nil {
		return err
	}

	return w.Close()
}

// 邮件模板
const expirationReminderTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #1890ff; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9f9f9; }
        .footer { padding: 20px; text-align: center; color: #999; font-size: 12px; }
        .btn { display: inline-block; padding: 10px 20px; background: #1890ff; color: white; text-decoration: none; border-radius: 4px; }
        .warning { color: #ff4d4f; font-weight: bold; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>授权到期提醒</h1>
        </div>
        <div class="content">
            <p>尊敬的 {{.UserName}}：</p>
            <p>您的 <strong>{{.AppName}}</strong> 授权即将到期：</p>
            <ul>
                <li>授权类型：{{.LicenseType}}</li>
                <li>到期时间：<span class="warning">{{.ExpireAt}}</span></li>
                <li>剩余天数：<span class="warning">{{.RemainingDays}} 天</span></li>
            </ul>
            <p>为避免影响您的正常使用，请及时续费。</p>
            <p style="text-align: center; margin-top: 30px;">
                <a href="{{.RenewURL}}" class="btn">立即续费</a>
            </p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿回复。</p>
        </div>
    </div>
</body>
</html>
`

// ExpirationReminderData 到期提醒数据
type ExpirationReminderData struct {
	UserName      string
	AppName       string
	LicenseType   string
	ExpireAt      string
	RemainingDays int
	RenewURL      string
}

// SendExpirationReminder 发送到期提醒
func (s *EmailService) SendExpirationReminder(to string, data ExpirationReminderData) error {
	tmpl, err := template.New("expiration").Parse(expirationReminderTemplate)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	subject := fmt.Sprintf("【授权到期提醒】您的 %s 授权将在 %d 天后到期", data.AppName, data.RemainingDays)
	return s.SendEmail(to, subject, buf.String())
}

// 激活成功模板
const activationSuccessTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #52c41a; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9f9f9; }
        .footer { padding: 20px; text-align: center; color: #999; font-size: 12px; }
        .success { color: #52c41a; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>激活成功</h1>
        </div>
        <div class="content">
            <p>尊敬的用户：</p>
            <p>您的 <strong>{{.AppName}}</strong> 已成功激活！</p>
            <ul>
                <li>授权类型：{{.LicenseType}}</li>
                <li>设备名称：{{.DeviceName}}</li>
                <li>激活时间：{{.ActivatedAt}}</li>
                <li>到期时间：{{.ExpireAt}}</li>
            </ul>
            <p class="success">感谢您的支持！</p>
        </div>
        <div class="footer">
            <p>此邮件由系统自动发送，请勿回复。</p>
        </div>
    </div>
</body>
</html>
`

// ActivationSuccessData 激活成功数据
type ActivationSuccessData struct {
	AppName     string
	LicenseType string
	DeviceName  string
	ActivatedAt string
	ExpireAt    string
}

// SendActivationSuccess 发送激活成功通知
func (s *EmailService) SendActivationSuccess(to string, data ActivationSuccessData) error {
	tmpl, err := template.New("activation").Parse(activationSuccessTemplate)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	subject := fmt.Sprintf("【激活成功】%s 已成功激活", data.AppName)
	return s.SendEmail(to, subject, buf.String())
}
