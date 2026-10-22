package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type eventInput struct {
	Description string    `form:"Description" binding:"required,max=12000"`
	Start       time.Time `form:"Start" time_format:"2006-01-02T15:04" binding:"required"`
	TimeZone    string    `form:"TimeZone" binding:"required"`
}

// Preserve the Nicevites override when testing locally.
func sitePath(c *gin.Context, path string) string {
	if strings.HasPrefix(strings.ToLower(c.Query("host")), "nice") {
		return path + "?host=nice"
	}
	return path
}

func (s *server) newInvitation(c *gin.Context) {
	s.creationForm(c, http.StatusOK, eventInput{}, "")
}

func (s *server) creationForm(c *gin.Context, status int, input eventInput, message string) {
	header := "What's going down?"
	if isNice(c) {
		header = "How may we have the immense pleasure of assisting you?"
	}
	c.HTML(status, "create.tmpl", gin.H{
		"title": "Make an event!", "headerText": header,
		"createAction": sitePath(c, "/event"), "aiEnabled": s.ai != nil,
		"input": input, "error": message,
	})
}

func (s *server) postEvent(c *gin.Context) {
	var input eventInput
	if err := c.ShouldBind(&input); err != nil {
		s.creationForm(c, http.StatusBadRequest, input, "Please include a description, date, time, and valid timezone.")
		return
	}
	event := Event{Description: input.Description, Markdown: input.Description, Start: input.Start, TimeZone: input.TimeZone, Voice: "aggro"}
	if err := event.Validate(); err != nil {
		s.creationForm(c, http.StatusBadRequest, input, "Please select a valid timezone.")
		return
	}
	if isNice(c) {
		event.Voice = "nice"
	}
	if s.ai != nil && c.PostForm("Generate") != "false" {
		if err := s.generateInvitation(c.Request.Context(), &event); err != nil {
			log.Printf("Invitation generation failed: %v", err)
			s.creationForm(c, http.StatusBadGateway, input, "We couldn't generate the invitation. Your details are below; try again or create it without AI.")
			return
		}
	}
	if err := s.db.Create(&event).Error; err != nil {
		errorPage(err, c)
		return
	}
	c.Redirect(http.StatusSeeOther, sitePath(c, fmt.Sprintf("/event/%d", event.ID)))
}

func (s *server) generateInvitation(ctx context.Context, event *Event) error {
	ctx, cancel := context.WithTimeout(ctx, 175*time.Second)
	defer cancel()
	generated, err := s.ai.Generate(ctx, *event)
	if err != nil {
		return err
	}
	event.Location = generated.Location
	event.Markdown = withDirections(generated.Markdown, generated.Location)
	if strings.TrimSpace(generated.ImagePrompt) == "" {
		generated.ImagePrompt = "A playful, text-free event illustration: " + event.Description
	}
	image, err := s.ai.Image(ctx, generated.ImagePrompt)
	if err != nil {
		log.Printf("Artwork generation failed: %v", err)
		return nil
	}
	event.Image = image
	return nil
}

func (s *server) eventImage(c *gin.Context) {
	var id struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&id); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	var event Event
	err := s.db.First(&event, id.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		errorPage(err, c)
		return
	}
	if len(event.Image) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "image/jpeg", event.Image)
}

func publicBaseURL(c *gin.Context) string {
	// Use known production origins instead of trusting arbitrary forwarded Host
	// headers in social previews. Local development uses its actual origin.
	if c.Request.Host == "nicevites.northbriton.net" || c.Request.Host == "aggrovites.northbriton.net" {
		return "https://" + c.Request.Host
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: c.Request.Host}).String()
}
