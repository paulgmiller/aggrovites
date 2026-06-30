package main

import (
	"errors"
	"fmt"

	//"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

type server struct {
	store Store
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
	store, err := openStore()
	if err != nil {
		panic(err)
	}

	s := server{store: store}

	router := gin.Default()
	//https://gin-gonic.com/docs/examples/html-rendering/
	//subdirectories did not seem to work here
	router.LoadHTMLGlob("templates/*")

	router.Static("/assets", "./assets")
	router.GET("/ready", func(c *gin.Context) {
		if err := store.Ready(); err != nil {
			errorPage(err, c)
			return
		}
		c.String(200, "READY")
	})

	router.GET("/", func(c *gin.Context) {
		template := "create.tmpl"
		htmlObj := gin.H{
			"title":              "Make an event!",
			"headerText":         "What's going down?",
			"startTimeFormLabel": "When?",
			"submitLabel":        "Let's fucking GO!",
		}

		if isNice(c) {
			htmlObj = gin.H{
				"title":              "Make an event!",
				"headerText":         "How can I help?",
				"startTimeFormLabel": "When?",
				"submitLabel":        "Let's make some magic!",
			}
		}
		c.HTML(http.StatusOK, template, htmlObj)
	})
	router.POST("/event", s.postEvent)
	router.GET("/event/:id", s.getEvent)

	router.POST("/rsvp", s.rsvp)

	log.Print(router.Run(":9000").Error())
}

func openStore() (Store, error) {
	cosmos, usingCosmos, err := newCosmosStoreFromEnv()
	if err != nil {
		return nil, err
	}
	if usingCosmos {
		log.Print("using cosmos db")
		return cosmos, nil
	}

	var data gorm.Dialector
	if mssqlDsn, found := os.LookupEnv("MSSQL_DSN"); found {
		log.Printf("using mssql %s", mssqlDsn)
		data = sqlserver.Open(mssqlDsn)
	} else {
		sqlitefile, found := os.LookupEnv("SQLLITE_FILE")
		if !found {
			sqlitefile = "test.db"
		}
		log.Printf("using sqllite db file %s", sqlitefile)
		data = sqlite.Open(sqlitefile)
	}

	db, err := gorm.Open(data, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}
	if err := db.AutoMigrate(&Event{}, &Rsvp{}); err != nil {
		return nil, err
	}
	return newGormStore(db), nil
}

func (s *server) postEvent(c *gin.Context) {
	var event Event
	if err := c.ShouldBind(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := event.Validate(); err != nil {
		errorPage(err, c)
		return
	}

	log.Printf("Storing event %v", event)
	if err := s.store.CreateEvent(&event); err != nil {
		errorPage(err, c)
		return
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/event/%s", event.RouteID()))
}

func (s *server) getEvent(c *gin.Context) {
	var id struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&id); err != nil {
		errorPage(err, c)
		return
	}

	event, err := s.store.GetEvent(id.ID)
	if errors.Is(err, errNotFound) {
		c.JSON(404, gin.H{"msg": "couldn't find your event"})
		return
	}
	if err != nil {
		errorPage(err, c)
		return
	}

	log.Printf("Got event %v", event)

	template := "event.tmpl"
	//should we validate this in some way to make sure we don't miss any fields?
	htmlObj := gin.H{
		"event": event,

		"ogTitle":                "Aggrovite",
		"ogUrl":                  fmt.Sprintf("https://aggrovites.northbriton.net/event/%s", event.RouteID()),
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
			"ogUrl":                  fmt.Sprintf("https://nicevites.northbriton.net/event/%s", event.RouteID()),
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

	c.HTML(http.StatusOK, template, htmlObj)
}

func (s *server) rsvp(c *gin.Context) {
	var rsvp Rsvp
	if err := c.ShouldBind(&rsvp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if rsvp.EventPublicID == "" && rsvp.EventID != 0 {
		rsvp.EventPublicID = fmt.Sprint(rsvp.EventID)
	}
	log.Printf("Got rsvp %v", rsvp)

	event, err := s.store.GetEvent(rsvp.EventPublicID)
	if errors.Is(err, errNotFound) {
		c.JSON(404, gin.H{"msg": "couldn't find your event"})
		return
	}
	if err != nil {
		errorPage(err, c)
		return
	}
	for _, existing := range event.Rsvps {
		if normalizeAttendee(existing.Attendee) == normalizeAttendee(rsvp.Attendee) {
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
	if err := s.store.CreateRsvp(&rsvp); err != nil {
		errorPage(err, c)
		return
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/event/%s", rsvp.EventPublicID))
}

func errorPage(err error, c *gin.Context) {
	log.Printf("ERROR: %s", err.Error())
	c.JSON(500, gin.H{"msg": err.Error()})
}
