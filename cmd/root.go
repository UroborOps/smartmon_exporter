package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"github.com/anatol/smart.go"
	"github.com/jaypipes/ghw"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/expfmt"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var (
	customRegistry = prometheus.NewRegistry()
	deviceInfo     = promauto.With(customRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Name: "smartmon_device_info",
		Help: "Information metric for the device with various attributes as labels",
	}, []string{"disk", "size_bytes", "storage_controller", "vendor", "model", "serial_number"})
)

var rootCmd = &cobra.Command{
	Use:   "smartmon_exporter",
	Short: "A brief description of your application",
	Long:  `A longer description of your application`,
	Run: func(cmd *cobra.Command, args []string) {
		outputFilePath, err := cmd.Flags().GetString("output")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		block, err := ghw.Block()
		if err != nil {
			log.Fatal(err)
		}

		for _, disk := range block.Disks {
			controller := disk.StorageController.String()
			subpath := disk.Name
			switch controller {
			case "SCSI":
				deviceInfo.WithLabelValues(disk.Name, string(disk.SizeBytes), disk.StorageController.String(), disk.Vendor, disk.Model, disk.SerialNumber).Set(1)
				dev, err := smart.OpenSata("/dev/" + subpath)
				fmt.Println("/dev/" + subpath)
				if err != nil {
					log.Fatal(err)
				}
				defer dev.Close()
				page, err := dev.ReadSMARTData()
				if err != nil {
					log.Fatal(err)
				}
				thr, err := dev.ReadSMARTThresholds()
				if err != nil {
					log.Fatal(err)
				}
				for id, a := range page.Attrs {
					name := strings.ReplaceAll(a.Name, "-", "_")
					if name == "" {
						continue
					}
					value := promauto.With(customRegistry).NewCounter(prometheus.CounterOpts{
						Name:        "smartmon_" + strings.ToLower(name) + "_value",
						
						ConstLabels: prometheus.Labels{"disk": disk.Name, "id": strconv.Itoa(int(a.Id)),},
					})
					value.Add(float64(a.Current))
					worst := promauto.With(customRegistry).NewCounter(prometheus.CounterOpts{
						Name:        "smartmon_" + strings.ToLower(name) + "_worst",
						ConstLabels: prometheus.Labels{"disk": disk.Name, "id": strconv.Itoa(int(a.Id))},
					})
					worst.Add(float64(a.Worst))
					threshold := promauto.With(customRegistry).NewCounter(prometheus.CounterOpts{
						Name:        "smartmon_" + strings.ToLower(name) + "_threshold",
						ConstLabels: prometheus.Labels{"disk": disk.Name, "id": strconv.Itoa(int(a.Id))},
					})
					threshold.Add(float64(thr.Thresholds[id]))
				}
			case "NVMe":
				dev, err := smart.OpenNVMe("/dev/" + subpath)
				if err != nil {
					log.Fatal(err)
				}
				defer dev.Close()
				sm, err := dev.ReadSMART()
				if err != nil {
					log.Fatal(err)
				}
				smValue := reflect.ValueOf(sm).Elem()
				re := regexp.MustCompile("([A-Z]+)")
				for i := 0; i < smValue.NumField(); i++ {
					field := smValue.Type().Field(i)
					replaced := re.ReplaceAllString(field.Name, "_$1")
					metric_name := "smartmon" + strings.ToLower(replaced)
					switch field.Type {
					case reflect.TypeOf(uint8(0)):
						promauto.With(customRegistry).NewCounter(prometheus.CounterOpts{
							Name:        metric_name,
							ConstLabels: prometheus.Labels{"disk": disk.Name},
						}).Add(float64(smValue.Field(i).Interface().(uint8)))
					case reflect.TypeOf(uint16(0)):
						promauto.With(customRegistry).NewCounter(prometheus.CounterOpts{
							Name:        metric_name,
							ConstLabels: prometheus.Labels{"disk": disk.Name},
						}).Add(float64(smValue.Field(i).Interface().(uint16)))
					case reflect.TypeOf(smart.Uint128{}):
						fieldValue := smValue.Field(i).Interface().(smart.Uint128)
						promauto.With(customRegistry).NewGauge(prometheus.GaugeOpts{
							Name:        metric_name,
							ConstLabels: prometheus.Labels{"disk": disk.Name},
						}).Add(float64(fieldValue.Val[0]))
		
					}
				}
			}
		}
		metricFamilies, err := customRegistry.Gather()
		if err != nil {
			log.Fatal(err)
		}
		if outputFilePath == "" {
			for _, mf := range metricFamilies {
				if _, err := expfmt.MetricFamilyToText(os.Stdout, mf); err != nil {
					log.Println("Error writing metrics to console:", err)
					return
				}
			}
		} else {
			file, err := os.Create("/var/lib/node_exporter/textfile_collector/smartmon.prom")
			if err != nil {
				log.Fatal(err)
			}
		defer file.Close()

		for _, mf := range metricFamilies {
			if _, err := expfmt.MetricFamilyToText(file, mf); err != nil {
			log.Fatal(err)
			}
		}
	}
	},
}



func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	var output string
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o","", "output to file")

}

func initConfig() {

}