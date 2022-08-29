package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//https://gorm.io/docs/has_many.html

func main() {

	sqllitefile, found := os.LookupEnv("SQLLITE_FILE")
	if !found {
		sqllitefile = "test.db"
	}
	log.Printf("using sqllite db file %s", sqllitefile)
	db, err := gorm.Open(sqlite.Open(sqllitefile), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Migrate the schema
	db.AutoMigrate(&Event{})
	db.AutoMigrate(&Rsvp{})

	router := gin.Default()
	//https://gin-gonic.com/docs/examples/bind-single-binary-with-template/
	t, err := template.ParseGlob("*.tmpl")
	if err != nil {
		log.Fatalf("couldnt load template, %s", err)
	}
	router.SetHTMLTemplate(t)
	router.Static("/assets", "./assets")
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "create.tmpl", gin.H{})
	})
	router.POST("/event", func(c *gin.Context) {
		var event Event
		if err := c.ShouldBind(&event); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("Storing event %v", event)
		if err := db.Create(&event).Error; err != nil {
			errorPage(err, c)
			return
		}

		c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d", event.ID))
	})
	router.GET("/event/:id", func(c *gin.Context) {
		var event Event
		var id struct {
			Id uint `uri:"id" binding:"required"`
		}
		if err := c.ShouldBindUri(&id); err != nil {
			errorPage(err, c)
			return
		}

		result := db.Model(&Event{}).Preload("Rsvps").Find(&event, id.Id)
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

		c.HTML(http.StatusOK, "event.tmpl", gin.H{"event": event})
	})
	router.POST("/rsvp", func(c *gin.Context) {
		var rsvp Rsvp
		if err := c.ShouldBind(&rsvp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("Got rsvp %v", rsvp)
		//TOOD make sure it point at a valid event?
		if err := db.Create(&rsvp).Error; err != nil {
			errorPage(err, c)
		}

		c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d", rsvp.EventID))
	})
	router.POST("/reject", func(c *gin.Context) {
		var rsvp Rsvp
		if err := c.ShouldBind(&rsvp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		rsvp.Declined = true
		log.Printf("Got rejection %v", rsvp)
		//TOOD make sure it point at a valid event?
		if err := db.Create(&rsvp).Error; err != nil {
			errorPage(err, c)
		}

		c.Redirect(http.StatusFound, fmt.Sprintf("/event/%d?sadness", rsvp.EventID))
	})
	log.Print(router.Run(":9000").Error())
}

func errorPage(err error, c *gin.Context) {
	log.Printf("ERROR: %s", err.Error())
	c.JSON(500, gin.H{"msg": err.Error()})
}
