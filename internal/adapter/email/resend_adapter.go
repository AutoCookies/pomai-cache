package email

import (
	"fmt"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/resend/resend-go/v3"
)

type ResendAdapter struct {
	client    *resend.Client
	fromEmail string
}

// NewResendAdapter khởi tạo adapter
func NewResendAdapter(apiKey string, fromEmail string) (ports.EmailSender, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("resend api key is missing")
	}

	// Fallback sender giống logic JS cũ
	if fromEmail == "" {
		fromEmail = "Cookiescooker <no-reply@cookiescooker.click>"
	}

	client := resend.NewClient(apiKey)
	return &ResendAdapter{
		client:    client,
		fromEmail: fromEmail,
	}, nil
}

func (r *ResendAdapter) SendVerificationEmail(to string, code string) error {
	subject := "Mã xác thực tài khoản POMAI"
	currentYear := time.Now().Year()

	// HTML Template (Converted from JS backticks to Go raw string)
	// Thay thế ${code} bằng %s và ${year} bằng %d
	htmlContent := fmt.Sprintf(`
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="utf-8">
        <title>Xác thực tài khoản POMAI</title>
    </head>
    <body style="background-color: #F9F7F2; font-family: 'Helvetica Neue', Helvetica, Arial, sans-serif; margin: 0; padding: 40px 0;">
        <table width="100%%" border="0" cellspacing="0" cellpadding="0">
            <tr>
                <td align="center">
                    
                    <table width="600" border="0" cellspacing="0" cellpadding="0" style="background-color: #ffffff; border-radius: 16px; overflow: hidden; box-shadow: 0 4px 20px rgba(0,0,0,0.05); border: 1px solid #EAEAEA;">
                        
                        <tr>
                            <td align="center" style="padding: 40px 0 20px 0; background-color: #ffffff; border-bottom: 2px solid #F9F7F2;">
                                <h1 style="color: #4B8B8B; font-family: Georgia, serif; font-size: 32px; letter-spacing: 0.2em; margin: 0; font-weight: bold;">POMAI</h1>
                                <p style="color: #E14D58; font-size: 10px; text-transform: uppercase; letter-spacing: 0.3em; margin-top: 5px; font-family: sans-serif;">Begin Your Story</p>
                            </td>
                        </tr>

                        <tr>
                            <td style="padding: 40px;">
                                <p style="color: #555555; font-size: 16px; line-height: 1.6; margin-bottom: 20px; text-align: center;">
                                    Xin chào, <br/>
                                    Để hoàn tất việc đăng ký và bảo vệ tài khoản của bạn, vui lòng sử dụng mã xác thực dưới đây:
                                </p>

                                <div style="background-color: #FFF5F5; border: 1px dashed #E14D58; border-radius: 12px; padding: 25px; text-align: center; margin: 30px 0;">
                                    <span style="color: #E14D58; font-size: 36px; font-family: monospace; letter-spacing: 8px; font-weight: bold; display: block;">%s</span>
                                </div>

                                <p style="color: #888888; font-size: 14px; line-height: 1.5; text-align: center; margin-bottom: 0;">
                                    Mã này sẽ hết hạn sau <strong>15 phút</strong>.<br/>
                                    Nếu bạn không yêu cầu mã này, vui lòng bỏ qua email.
                                </p>
                            </td>
                        </tr>

                        <tr>
                            <td align="center" style="background-color: #2C2C2C; padding: 20px;">
                                <p style="color: #F9F7F2; font-size: 12px; margin: 0; font-family: sans-serif;">© %d POMAI Workspace. All rights reserved.</p>
                            </td>
                        </tr>
                    </table>

                    <p style="text-align: center; margin-top: 20px; color: #4B8B8B; opacity: 0.6; font-size: 12px; font-family: sans-serif;">Secured by Pomai Auth System</p>
                </td>
            </tr>
        </table>
    </body>
    </html>
  `, code, currentYear)

	params := &resend.SendEmailRequest{
		From:    r.fromEmail,
		To:      []string{to},
		Subject: subject,
		Html:    htmlContent,
	}

	_, err := r.client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("failed to send email via resend: %w", err)
	}

	return nil
}
