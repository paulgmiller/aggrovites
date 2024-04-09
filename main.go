package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/gin-gonic/gin"
	_ "github.com/microsoft/gocosmos"
	"github.com/samber/lo"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

//https://gorm.io/docs/has_many.html

func main() {

	/* Coulnt' get this to work would have to write a gorm driver or move away from gorm.
	driver := "gocosmos"
	dsn := "AccountEndpoint=https://vitesdb.documents.azure.com:443/;AccountKey=<key>;DefaultDb=vites"
	cosmos, err := sql.Open(driver, dsn)
	if err != nil {
		panic(err)
	}
	defer cosmos.Close()
	*/
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

	client, err := azcosmos.NewClientFromConnectionString(os.Getenv("COSMOS_CONNECTION_STRING"), &azcosmos.ClientOptions{})
	if err != nil {
		panic(err)
	}

	vites := lo.Must(client.NewContainer("letsmeeup", "vites"))

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

		bin64 rand.Int63()

		partitionKey := azcosmos.NewPartitionKeyString("gear-surf-surfboards")

		context := context.TODO()

		bytes, err := json.Marshal(item)
		if err != nil {
			return err
		}

		vites.Upsert()
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
