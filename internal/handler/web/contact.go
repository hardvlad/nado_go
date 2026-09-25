package web

import (
	"net/http"

	"nado_go/internal/httpx"
	"nado_go/internal/i18n"
	"nado_go/internal/model"
	"nado_go/internal/service"
)

// TopicOption — пункт выбора темы обращения.
type TopicOption struct {
	Value string
	Label string
}

func topicOptions(l *i18n.Localizer) []TopicOption {
	topics := model.FeedbackTopics()
	out := make([]TopicOption, len(topics))
	for i, t := range topics {
		out[i] = TopicOption{Value: t, Label: l.T("contact.topic." + t)}
	}
	return out
}

// ContactForm — GET /contact.
func (h *PageHandler) ContactForm(w http.ResponseWriter, r *http.Request) error {
	form := newForm()
	form.Values["topic"] = httpx.QueryString(r, "topic", model.FeedbackQuestion)
	return h.renderContact(w, r, http.StatusOK, form, r.URL.Query().Get("sent") == "1")
}

// ContactSubmit — POST /contact.
func (h *PageHandler) ContactSubmit(w http.ResponseWriter, r *http.Request) error {
	if err := parseForm(w, r); err != nil {
		return err
	}
	l := h.localizer(r)
	sentURL := i18n.Localize(l.Lang(), "/contact") + "?sent=1"

	// Ловушка для ботов: поле скрыто от людей, а боты заполняют все поля.
	// Делаем вид, что всё прошло успешно, — бот не узнает, что его распознали.
	if r.PostFormValue("website") != "" {
		redirect(w, r, sentURL)
		return nil
	}

	form := newForm()
	for _, f := range []string{"name", "contact", "topic", "message"} {
		form.Values[f] = r.PostFormValue(f)
	}
	form.Checked["consent"] = r.PostFormValue("consent") == "on"

	if !h.feedbackByIP.Allow("feedback:" + httpx.ClientIP(r)) {
		form.Alert = l.T("error.429.text")
		return h.renderContact(w, r, http.StatusTooManyRequests, form, false)
	}

	_, err := h.Feedback.Submit(r.Context(), service.FeedbackInput{
		Name:    form.Values["name"],
		Contact: form.Values["contact"],
		Topic:   form.Values["topic"],
		Message: form.Values["message"],
		Consent: form.Checked["consent"],
		Lang:    string(l.Lang()),
		Client:  clientMeta(r),
	})
	if fields, ok := httpx.FieldErrors(err); ok {
		form.Errors = localizeFields(l, fields)
		form.Alert = l.T("form.errors")
		return h.renderContact(w, r, http.StatusUnprocessableEntity, form, false)
	}
	if err != nil {
		return err
	}

	redirect(w, r, sentURL)
	return nil
}

func (h *PageHandler) renderContact(w http.ResponseWriter, r *http.Request, status int, form FormView, sent bool) error {
	l := h.localizer(r)
	data := h.page(r, "contact.title").
		With("Form", form).
		With("Sent", sent).
		With("Topics", topicOptions(l))
	return h.Render.Render(w, status, "contact", data)
}
