package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InvitationDraft struct {
	Token           string    `gorm:"primaryKey;size:64"`
	ExpiresAt       time.Time `gorm:"index"`
	Description     string
	Start           time.Time
	TimeZone        string
	Voice           string
	Markdown        string
	Location        string
	Image           []byte
	IncludeImage    bool
	Notice          string
	EventID         uint
	GeneratingUntil time.Time
}

type eventInput struct {
	Description string    `form:"Description" binding:"required,max=12000"`
	Start       time.Time `form:"Start" time_format:"2006-01-02T15:04" binding:"required"`
	TimeZone    string    `form:"TimeZone" binding:"required"`
}

func (input eventInput) event() Event {
	return Event{Description: input.Description, Start: input.Start, TimeZone: input.TimeZone}
}

func (draft InvitationDraft) event() Event {
	return Event{
		Description: draft.Description, Start: draft.Start, TimeZone: draft.TimeZone,
		Voice: draft.Voice, Markdown: draft.Markdown, Location: draft.Location, Image: draft.Image,
	}
}

// The query override is useful for running Nicevites locally. Carry it through
// every form and redirect instead of losing it after the first submission.
func sitePath(c *gin.Context, path string) string {
	if strings.HasPrefix(strings.ToLower(c.Query("host")), "nice") {
		return path + "?host=nice"
	}
	return path
}

func (s *server) newInvitation(c *gin.Context) {
	data := gin.H{
		"title": "Make an event!", "headerText": "What's going down?",
		"previewAction": sitePath(c, "/event/preview"), "aiEnabled": s.ai != nil,
	}
	if isNice(c) {
		data["headerText"] = "How may we have the immense pleasure of assisting you?"
	}
	c.HTML(http.StatusOK, "create.tmpl", data)
}

func (s *server) previewInvitation(c *gin.Context) {
	var input eventInput
	if err := c.ShouldBind(&input); err != nil {
		s.creationError(c, input, "Please include a description, date, time, and valid timezone.")
		return
	}
	event := input.event()
	if err := event.Validate(); err != nil {
		s.creationError(c, input, "Please select a valid timezone.")
		return
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		errorPage(err, c)
		return
	}
	voice := "aggro"
	if isNice(c) {
		voice = "nice"
	}
	draft := InvitationDraft{
		Token: hex.EncodeToString(token), ExpiresAt: time.Now().Add(24 * time.Hour),
		Description: input.Description, Start: input.Start, TimeZone: input.TimeZone, Voice: voice,
		Markdown: input.Description,
	}
	// Expired private drafts are no longer useful, including their image bytes.
	if err := s.db.Where("expires_at < ?", time.Now()).Delete(&InvitationDraft{}).Error; err != nil {
		errorPage(err, c)
		return
	}
	if err := s.db.Create(&draft).Error; err != nil {
		errorPage(err, c)
		return
	}
	if c.PostForm("Generate") == "true" {
		if err := s.generateDraft(c, &draft); err != nil {
			errorPage(err, c)
			return
		}
	}
	c.Redirect(http.StatusSeeOther, sitePath(c, "/draft/"+draft.Token))
}

func (s *server) creationError(c *gin.Context, input eventInput, message string) {
	c.HTML(http.StatusBadRequest, "create.tmpl", gin.H{
		"title": "Make an event!", "headerText": "Let's get the details right",
		"previewAction": sitePath(c, "/event/preview"), "aiEnabled": s.ai != nil,
		"input": input, "error": message,
	})
}

func (s *server) loadDraft(c *gin.Context) (InvitationDraft, bool) {
	var draft InvitationDraft
	err := s.db.Where("token = ? AND expires_at > ?", c.Param("token"), time.Now()).First(&draft).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.String(http.StatusNotFound, "This draft has expired or does not exist. Please create a new invitation.")
		return draft, false
	}
	if err != nil {
		errorPage(err, c)
		return draft, false
	}
	return draft, true
}

func (s *server) getDraft(c *gin.Context) {
	draft, ok := s.loadDraft(c)
	if !ok {
		return
	}
	if draft.EventID != 0 {
		c.Redirect(http.StatusSeeOther, sitePath(c, fmt.Sprintf("/event/%d", draft.EventID)))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.HTML(http.StatusOK, "preview.tmpl", gin.H{
		"draft": draft, "preview": renderMarkdown(draft.Markdown), "aiEnabled": s.ai != nil,
		"draftAction": sitePath(c, "/draft/"+draft.Token), "publishAction": sitePath(c, "/event"),
		"imageURL": "/draft/" + draft.Token + "/image",
	})
}

func (s *server) updateDraft(c *gin.Context) {
	draft, ok := s.loadDraft(c)
	if !ok {
		return
	}
	if draft.EventID != 0 {
		c.Redirect(http.StatusSeeOther, sitePath(c, fmt.Sprintf("/event/%d", draft.EventID)))
		return
	}
	if c.PostForm("Action") == "regenerate" {
		var input eventInput
		if err := c.ShouldBind(&input); err != nil || input.event().Validate() != nil {
			c.String(http.StatusBadRequest, "Please include a description, date, time, and valid timezone.")
			return
		}
		draft.Description, draft.Start, draft.TimeZone = input.Description, input.Start, input.TimeZone
		// Save host edits before a potentially slow API call. A provider failure
		// leaves both the original input and edited Markdown available.
		if !s.saveDraftEdits(c, &draft) {
			return
		}
		if err := s.generateDraft(c, &draft); err != nil {
			errorPage(err, c)
			return
		}
	} else if !s.saveDraftEdits(c, &draft) {
		return
	}
	c.Redirect(http.StatusSeeOther, sitePath(c, "/draft/"+draft.Token))
}

func (s *server) saveDraftEdits(c *gin.Context, draft *InvitationDraft) bool {
	value := strings.TrimSpace(c.PostForm("Markdown"))
	if value == "" || len(value) > maxMarkdown {
		c.String(http.StatusBadRequest, "Please enter Markdown up to 24,000 bytes.")
		return false
	}
	result := s.db.Model(&InvitationDraft{}).
		Where("token = ? AND event_id = 0 AND generating_until < ?", draft.Token, time.Now()).
		Updates(map[string]any{
			"markdown": value, "description": draft.Description, "start": draft.Start, "time_zone": draft.TimeZone,
			"include_image": c.PostForm("IncludeImage") == "true",
		})
	if result.Error != nil {
		errorPage(result.Error, c)
		return false
	}
	if result.RowsAffected != 1 {
		c.String(http.StatusConflict, "This invitation is being generated or has already been published. Please refresh the preview.")
		return false
	}
	draft.Markdown = value
	draft.IncludeImage = c.PostForm("IncludeImage") == "true"
	return true
}

func (s *server) generateDraft(c *gin.Context, draft *InvitationDraft) error {
	// A database lease prevents simultaneous regeneration requests for the same
	// draft, including requests handled by different application replicas.
	claim := s.db.Model(&InvitationDraft{}).
		Where("token = ? AND event_id = 0 AND generating_until < ?", draft.Token, time.Now()).
		Update("generating_until", time.Now().Add(3*time.Minute))
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected != 1 {
		return errDraftUnavailable
	}
	notice := ""
	if s.ai == nil {
		notice = "AI generation is unavailable. You can edit and publish the Markdown yourself."
	} else {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 175*time.Second)
		defer cancel()
		generated, err := s.ai.Generate(ctx, draft.event())
		if err != nil {
			log.Printf("Invitation generation failed: %v", err)
			notice = "We couldn't generate the invitation. Your text is saved. Retry or edit the Markdown and publish manually."
		} else {
			draft.Location = generated.Location
			draft.Markdown = withDirections(generated.Markdown, generated.Location)
			draft.Image = nil
			draft.IncludeImage = false
			if strings.TrimSpace(generated.ImagePrompt) == "" {
				generated.ImagePrompt = "A playful, text-free event illustration: " + draft.Description
			}
			image, err := s.ai.Image(ctx, generated.ImagePrompt)
			if err != nil {
				log.Printf("Artwork generation failed: %v", err)
				notice = "Your invitation is ready, but artwork couldn't be generated. You can publish without artwork or regenerate."
			} else {
				draft.Image = image
				draft.IncludeImage = true
			}
		}
	}
	return s.db.Model(&InvitationDraft{}).Where("token = ? AND event_id = 0", draft.Token).
		Updates(map[string]any{
			"markdown": draft.Markdown, "location": draft.Location, "image": draft.Image,
			"include_image": draft.IncludeImage,
			"notice":        notice, "generating_until": time.Time{},
		}).Error
}

var errDraftUnavailable = errors.New("draft expired, missing, or still generating")

func (s *server) publishDraft(c *gin.Context) {
	var eventID uint
	markdown := strings.TrimSpace(c.PostForm("Markdown"))
	if markdown == "" || len(markdown) > maxMarkdown {
		c.String(http.StatusBadRequest, "Please enter Markdown up to 24,000 bytes.")
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var draft InvitationDraft
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token = ? AND expires_at > ?", c.PostForm("DraftToken"), time.Now()).First(&draft).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errDraftUnavailable
		}
		if err != nil {
			return err
		}
		if draft.EventID != 0 {
			eventID = draft.EventID
			return nil
		}
		if draft.GeneratingUntil.After(time.Now()) {
			return errDraftUnavailable
		}
		event := draft.event()
		event.Markdown = markdown
		if c.PostForm("IncludeImage") != "true" {
			event.Image = nil
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		eventID = event.ID
		return tx.Model(&draft).Updates(map[string]any{"event_id": eventID, "image": nil}).Error
	})
	if errors.Is(err, errDraftUnavailable) {
		c.String(http.StatusConflict, "This draft has expired, does not exist, or is still generating. Please return to the preview.")
		return
	}
	if err != nil {
		errorPage(err, c)
		return
	}
	c.Redirect(http.StatusSeeOther, sitePath(c, fmt.Sprintf("/event/%d", eventID)))
}

func (s *server) draftImage(c *gin.Context) {
	draft, ok := s.loadDraft(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	serveImage(c, draft.Image)
}

func serveImage(c *gin.Context, image []byte) {
	if len(image) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "image/jpeg", image)
}

func (s *server) loadEvent(c *gin.Context) (Event, bool) {
	var id struct {
		ID uint `uri:"id" binding:"required"`
	}
	var event Event
	if err := c.ShouldBindUri(&id); err != nil {
		c.Status(http.StatusBadRequest)
		return event, false
	}
	err := s.db.First(&event, id.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(http.StatusNotFound)
		return event, false
	}
	if err != nil {
		errorPage(err, c)
		return event, false
	}
	return event, true
}

func (s *server) eventImage(c *gin.Context) {
	if event, ok := s.loadEvent(c); ok {
		serveImage(c, event.Image)
	}
}

func (s *server) downloadMarkdown(c *gin.Context) {
	if event, ok := s.loadEvent(c); ok {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="invitation-%d.md"`, event.ID))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(event.ExportMarkdown(publicBaseURL(c))))
	}
}

func publicBaseURL(c *gin.Context) string {
	// Use known production origins instead of trusting arbitrary forwarded Host
	// headers in exported invitations. Local development uses its actual origin.
	if c.Request.Host == "nicevites.northbriton.net" || c.Request.Host == "aggrovites.northbriton.net" {
		return "https://" + c.Request.Host
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: c.Request.Host}).String()
}
