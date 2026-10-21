package main

import (
	"fmt"

	//"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type server struct {
	db *gorm.DB
	ai invitationGenerator
}

//https://gorm.io/docs/has_many.html

func isNice(c *gin.Context) bool {
	if strings.HasPrefix(strings.ToLower(c.Request.Host), "nice") {
		return true
	}
	//log.Printf("host query arg %s", c.Request.URL.Query().Get("host"))
	if strings.HasPrefix(strings.ToLower(c.Request.URL.Query().Get("host")), "nice") {
		return true
	}
	return false
}

func main() {
	db, err := openDestination()
	if err != nil {
		log.Fatal(err)
	}

	if err := migrateSchema(db); err != nil {
		log.Fatal(err)
	}
	s := &server{db: db, ai: configuredAI()}
	log.Print(newRouter(s).Run(":9000"))
}

func newRouter(s *server) *gin.Engine {
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		if c.Request.Method == http.MethodPost {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
		}
		c.Next()
	})
	//https://gin-gonic.com/docs/examples/html-rendering/
	//subdirectories did not seem to work here
	router.LoadHTMLGlob("templates/*")

	router.Static("/assets", "./assets")
	router.GET("/ready", func(c *gin.Context) {
		actualdb, err := s.db.DB()
		if err != nil {
			errorPage(err, c)
			return
		}
		if err := actualdb.Ping(); err != nil {
			errorPage(err, c)
			return
		}
		c.String(200, "READY")
	})

	router.GET("/", s.newInvitation)
	router.POST("/event/preview", s.previewInvitation)
	router.GET("/draft/:token", s.getDraft)
	router.POST("/draft/:token", s.updateDraft)
	router.GET("/draft/:token/image", s.draftImage)
	router.POST("/event", s.postEvent)
	router.GET("/event/:id", s.getEvent)
	router.GET("/event/:id/image", s.eventImage)
	router.GET("/event/:id/invitation.md", s.downloadMarkdown)

	router.POST("/rsvp", s.rsvp)

	return router
}

func (s *server) postEvent(c *gin.Context) {
	if c.PostForm("DraftToken") != "" {
		s.publishDraft(c)
		return
	}
	// Keep the legacy plain-text endpoint, binding only host-editable fields.
	var input eventInput
	if err := c.ShouldBind(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	event := input.event()

	if err := event.Validate(); err != nil {
		errorPage(err, c)
		return
	}

	if err := s.db.Create(&event).Error; err != nil {
		errorPage(err, c)
		return
	}

	c.Redirect(http.StatusFound, sitePath(c, fmt.Sprintf("/event/%d", event.ID)))
}

func (s *server) getEvent(c *gin.Context) {
	var event Event
	var id struct {
		Id uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&id); err != nil {
		errorPage(err, c)
		return
	}

	result := s.db.Model(&Event{}).Preload("Rsvps").Find(&event, id.Id)
	log.Printf("result: %v", result)
	if result.Error != nil {
		errorPage(result.Error, c)
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(404, gin.H{"msg": "couldn't find your event"})
		return
	}

	template := "event.tmpl"
	//should we validate this in some way to make sure we don't miss any fields?
	htmlObj := gin.H{
		"event": event,

		"ogTitle":                "Aggrovite",
		"ogUrl":                  fmt.Sprintf("https://aggrovites.northbriton.net/event/%d", event.ID),
		"ogImageUrl":             "https://aggrovites.northbriton.net/assets/aggrovites.jpg",
		"title":                  "Holler Back",
		"rsvpHeader":             "Bitch you coming?",
		"rsvpForWhom":            "Who you?",
		"rsvpAccept":             "fuck yeah",
		"rsvpDecline":            "hell no",
		"rsvpGuestCountHeader":   "How many you bringing?",
		"exportEventHeader":      "Write it down knuckle head",
		"rsvpAcceptedListHeader": "Fabulous People:",
		"rsvpDeclinedListHeader": "Losers:",
	}
	if isNice(c) {
		htmlObj = gin.H{
			"event": event,

			"ogTitle":                "Nicevite",
			"ogUrl":                  fmt.Sprintf("https://nicevites.northbriton.net/event/%d", event.ID),
			"ogImageUrl":             "https://nicevites.northbriton.net/assets/nicevites.jpg",
			"title":                  "Répondez s'il vous plaît",
			"rsvpHeader":             "Be delighted to have you",
			"rsvpAccept":             "My pleasure",
			"rsvpDecline":            "Sadly not",
			"rsvpForWhom":            "How would you like to be addressed?",
			"rsvpGuestCountHeader":   "How many will bless us?",
			"exportEventHeader":      "A polite reminder",
			"rsvpAcceptedListHeader": "Lucky to have:",
			"rsvpDeclinedListHeader": "Regretfully Absent:",
		}
	}

	htmlObj["ogDescription"] = event.PlainDescription()
	htmlObj["rsvpAction"] = sitePath(c, "/rsvp")
	htmlObj["markdownURL"] = sitePath(c, fmt.Sprintf("/event/%d/invitation.md", event.ID))
	if event.Markdown != "" {
		htmlObj["ogTitle"] = event.Title()
		htmlObj["title"] = event.Title()
	}
	if len(event.Image) != 0 {
		htmlObj["ogImageUrl"] = fmt.Sprintf("%s/event/%d/image", publicBaseURL(c), event.ID)
	}
	htmlObj["ogUrl"] = fmt.Sprintf("%s/event/%d", publicBaseURL(c), event.ID)
	c.HTML(http.StatusOK, template, htmlObj)
}

func (s *server) rsvp(c *gin.Context) {
	var rsvp Rsvp
	if err := c.ShouldBind(&rsvp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("Got rsvp %v", rsvp)

	var event Event
	result := s.db.Model(&Event{}).Preload("Rsvps").Find(&event, rsvp.EventID)
	if result.Error != nil {
		errorPage(result.Error, c)
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(404, gin.H{"msg": "couldn't find your event"})
		return
	}
	for _, existing := range event.Rsvps {
		if strings.EqualFold(existing.Attendee, rsvp.Attendee) {
			log.Printf("Found existing rsvp %s updating count from ", rsvp.Attendee)
			//a json page is probably not the best experience
			errorPage(fmt.Errorf("already got an RSVP for %s", rsvp.Attendee), c)
			/* Alternatively could let anyone update... or only update if they have a cookie of who rsvpd?
			rsvp.ID = existing.ID
			if err := s.db.Save(&rsvp).Error; err != nil {
				errorPage(err, c)
				return
			}
			c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d", rsvp.EventID))*/
			return
		}
	}
	//TOOD make sure it point at a valid event?
	if err := s.db.Create(&rsvp).Error; err != nil {
		errorPage(err, c)
		return
	}

	c.Redirect(http.StatusFound, sitePath(c, fmt.Sprintf("/event/%d", rsvp.EventID)))
}

func errorPage(err error, c *gin.Context) {
	log.Printf("ERROR: %s", err.Error())
	c.JSON(500, gin.H{"msg": err.Error()})
}
