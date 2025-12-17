package ports

// EmailSender quy định hành vi gửi email
type EmailSender interface {
	SendVerificationEmail(to string, code string) error
}
