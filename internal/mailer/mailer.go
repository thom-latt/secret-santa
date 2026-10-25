package mailer

import (
	"fmt"
	"strings"

	"secret-santa/internal/config"
)

type Message struct {
	To      string
	Subject string
	Body    string
}

type Mailer interface {
	Send(msg Message) error
	SendBatch(msgs []Message) []error
}

func NewMailer(cfg *config.Config) Mailer {
	if cfg.AppEnv == "development" {
		return NewMockMailer()
	}
	return NewSMTPMailer(cfg)
}

type AdminMagicLinkData struct {
	OrganizerName string
	EventTitle    string
	AdminToken    string
	InvitedByAdmin bool
}

func BuildAdminMagicLinkEmail(baseURL, locale, to string, data AdminMagicLinkData) Message {
	link := strings.TrimRight(baseURL, "/") + "/admin/events/" + data.AdminToken
	if locale == "en" {
		subject := fmt.Sprintf("Secret Santa — Manage your event \"%s\"", data.EventTitle)
		intro := fmt.Sprintf("Hello %s,\n\nYour Secret Santa event \"%s\" has been created!", data.OrganizerName, data.EventTitle)
		if data.InvitedByAdmin {
			intro = fmt.Sprintf("Hello %s,\n\nAn administrator has invited you to organize the Secret Santa event \"%s\"!", data.OrganizerName, data.EventTitle)
		}
		body := fmt.Sprintf("%s\n\nUse your secret administration link below to add participants, set exclusions, and launch the draw:\n%s\n\nKeep this link safe — no password is required.", intro, link)
		return Message{To: to, Subject: subject, Body: body}
	}

	subject := fmt.Sprintf("Secret Santa — Gérez votre événement \"%s\"", data.EventTitle)
	intro := fmt.Sprintf("Bonjour %s,\n\nVotre événement Secret Santa \"%s\" a bien été créé !", data.OrganizerName, data.EventTitle)
	if data.InvitedByAdmin {
		intro = fmt.Sprintf("Bonjour %s,\n\nUn administrateur vous invite à organiser l'événement Secret Santa \"%s\" !", data.OrganizerName, data.EventTitle)
	}
	body := fmt.Sprintf("%s\n\nUtilisez votre lien secret d'administration ci-dessous pour ajouter les participants, définir les exclusions et lancer le tirage :\n%s\n\nConservez précieusement ce lien — aucun mot de passe n'est requis.", intro, link)
	return Message{To: to, Subject: subject, Body: body}
}

type ParticipantDrawData struct {
	ParticipantName string
	EventTitle      string
	EventDate       string
	Budget          string
	Description     string
	Token           string
}

func BuildParticipantDrawEmail(baseURL, locale, to string, data ParticipantDrawData) Message {
	link := strings.TrimRight(baseURL, "/") + "/p/" + data.Token
	if locale == "en" {
		subject := fmt.Sprintf("Secret Santa — Your draw for \"%s\"", data.EventTitle)
		var details strings.Builder
		if data.EventDate != "" {
			details.WriteString(fmt.Sprintf("- Date: %s\n", data.EventDate))
		}
		if data.Budget != "" {
			details.WriteString(fmt.Sprintf("- Suggested budget: %s\n", data.Budget))
		}
		if data.Description != "" {
			details.WriteString(fmt.Sprintf("- Note from organizer: %s\n", data.Description))
		}
		extra := ""
		if details.Len() > 0 {
			extra = "\nEvent details:\n" + details.String()
		}
		body := fmt.Sprintf("Hello %s,\n\nThe Secret Santa draw for \"%s\" has taken place!%s\nClick your personal secret link below to discover who you drew and fill in your wishlist:\n%s\n", data.ParticipantName, data.EventTitle, extra, link)
		return Message{To: to, Subject: subject, Body: body}
	}

	subject := fmt.Sprintf("Secret Santa — Votre tirage pour \"%s\"", data.EventTitle)
	var details strings.Builder
	if data.EventDate != "" {
		details.WriteString(fmt.Sprintf("- Date : %s\n", data.EventDate))
	}
	if data.Budget != "" {
		details.WriteString(fmt.Sprintf("- Budget indicatif : %s\n", data.Budget))
	}
	if data.Description != "" {
		details.WriteString(fmt.Sprintf("- Message de l'organisateur : %s\n", data.Description))
	}
	extra := ""
	if details.Len() > 0 {
		extra = "\nDétails de l'événement :\n" + details.String()
	}
	body := fmt.Sprintf("Bonjour %s,\n\nLe tirage au sort Secret Santa pour \"%s\" a eu lieu !%s\nCliquez sur votre lien personnel secret ci-dessous pour découvrir à qui vous offrez un cadeau et remplir votre liste de souhaits :\n%s\n", data.ParticipantName, data.EventTitle, extra, link)
	return Message{To: to, Subject: subject, Body: body}
}

type RecoveryItem struct {
	Title string
	URL   string
}

func BuildRecoveryEmail(baseURL, locale, to string, orgLinks, partLinks []RecoveryItem) Message {
	if locale == "en" {
		var sb strings.Builder
		sb.WriteString("Hello,\n\nHere are the active Secret Santa links associated with your email address:\n")
		if len(orgLinks) > 0 {
			sb.WriteString("\nEvents you organize (Administration):\n")
			for _, item := range orgLinks {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Title, item.URL))
			}
		}
		if len(partLinks) > 0 {
			sb.WriteString("\nEvents you participate in:\n")
			for _, item := range partLinks {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Title, item.URL))
			}
		}
		return Message{
			To:      to,
			Subject: "Secret Santa — Your active magic links",
			Body:    sb.String(),
		}
	}

	var sb strings.Builder
	sb.WriteString("Bonjour,\n\nVoici les liens actifs Secret Santa associés à votre adresse email :\n")
	if len(orgLinks) > 0 {
		sb.WriteString("\nÉvénements que vous organisez (Administration) :\n")
		for _, item := range orgLinks {
			sb.WriteString(fmt.Sprintf("- %s : %s\n", item.Title, item.URL))
		}
	}
	if len(partLinks) > 0 {
		sb.WriteString("\nÉvénements auxquels vous participez :\n")
		for _, item := range partLinks {
			sb.WriteString(fmt.Sprintf("- %s : %s\n", item.Title, item.URL))
		}
	}
	return Message{
		To:      to,
		Subject: "Secret Santa — Vos liens magiques actifs",
		Body:    sb.String(),
	}
}
