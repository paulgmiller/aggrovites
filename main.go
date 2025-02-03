package main

import (
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
	db *gorm.DB
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
	var data gorm.Dialector
	if mssql_dsn, found := os.LookupEnv("MSSQL_DSN"); found {
		log.Printf("using mssql %s", mssql_dsn)
		data = sqlserver.Open(mssql_dsn)
	} else {
		sqllitefile, found := os.LookupEnv("SQLLITE_FILE")
		if !found {
			sqllitefile = "test.db"
		}
		log.Printf("using sqllite db file %s", sqllitefile)
		data = sqlite.Open(sqllitefile)
	}

	db, err := gorm.Open(data, &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Migrate the schema
	db.AutoMigrate(&Event{})
	db.AutoMigrate(&Rsvp{})
	s := server{db}

	router := gin.Default()
	//https://gin-gonic.com/docs/examples/html-rendering/
	//subdirectories did not seem to work here
	router.LoadHTMLGlob("templates/*")

	router.Static("/assets", "./assets")
	router.GET("/ready", func(c *gin.Context) {
		actualdb, err := db.DB()
		if err != nil {
			errorPage(err, c)
		}
		if err := actualdb.Ping(); err != nil {
			errorPage(err, c)
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
	router.POST("/reject", s.reject)

	log.Print(router.Run(":9000").Error())
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
	if err := s.db.Create(&event).Error; err != nil {
		errorPage(err, c)
		return
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d", event.ID))
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

	log.Printf("Got event %v", event)

	template := "event.tmpl"
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

	c.HTML(http.StatusOK, template, htmlObj)
}

func (s *server) rsvp(c *gin.Context) {
	var rsvp Rsvp
	if err := c.ShouldBind(&rsvp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("Got rsvp %v", rsvp)
	//TOOD make sure it point at a valid event?
	if err := s.db.Create(&rsvp).Error; err != nil {
		errorPage(err, c)
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d", rsvp.EventID))
}

func (s *server) reject(c *gin.Context) {
	var rsvp Rsvp
	if err := c.ShouldBind(&rsvp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rsvp.Declined = true
	log.Printf("Got rejection %v", rsvp)
	//TOOD make sure it point at a valid event?
	if err := s.db.Create(&rsvp).Error; err != nil {
		errorPage(err, c)
	}

	c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d?sadness", rsvp.EventID))
}

func errorPage(err error, c *gin.Context) {
	log.Printf("ERROR: %s", err.Error())
	c.JSON(500, gin.H{"msg": err.Error()})
}
