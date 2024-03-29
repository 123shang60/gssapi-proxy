package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/123shang60/spnego-proxy/internal/common"
	"github.com/123shang60/spnego-proxy/internal/config"
	"github.com/123shang60/spnego-proxy/internal/proxy"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	ginprometheus "github.com/mcuadros/go-gin-prometheus"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var Server = &cobra.Command{
	Use:   "server",
	Short: "Run the spnego-proxy server",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		common.SetLogger()

		return nil
	},
	Run: Run,
}

func init() {
	Server.Flags().StringVar(&config.C.Porxy.TargetUrl, "target-url", "", "The target URL to proxy requests to")

	config.C.Porxy.TargetUrl = strings.TrimSuffix(config.C.Porxy.TargetUrl, "/")

	Server.Flags().StringVar(&config.C.Auth.KeyTabPath, "keytab-path", "", "The path to the keytab file")
	Server.Flags().StringVar(&config.C.Auth.KerberosConfigPath, "kerberos-config-path", "/etc/krb5.conf", "The path to the kerberos config file")
	Server.Flags().StringVar(&config.C.Auth.ServiceName, "gssapi-servicename", "HTTP", "The gssapi service name")
	Server.Flags().StringVar(&config.C.Auth.UserName, "gssapi-username", "HTTP", "The gssapi user name")
	Server.Flags().StringVar(&config.C.Auth.Realm, "realm", "", "The realm")
	Server.Flags().BoolVar(&config.C.Auth.DisablePAFXFAST, "disable-pafx-fast", false, "Disable the use of PA-FX-FAST")
	Server.Flags().StringToStringVar(&config.C.Auth.SPNHostsMapping, "spn-hosts-mapping", nil, "A mapping of SPNs to hosts")
	Server.Flags().StringVar(&config.C.Log.Level, "log-level", "info", "The log level")
	Server.Flags().Int32Var(&config.C.Server.Port, "port", 8080, "The port to listen on")
	Server.Flags().BoolVar(&config.C.Server.EnablePprof, "enable-pprof", false, "Enable pprof")
	Server.Flags().BoolVar(&config.C.Server.EnablePrometheus, "enable-prometheus", false, "Enable prometheus target")
}

func Run(_ *cobra.Command, _ []string) {
	configRaw, _ := json.Marshal(config.C)
	logrus.Infof("启动配置: %s", string(configRaw))

	// 先认证 krb5
	if err := proxy.InitKrb5Cli(); err != nil {
		logrus.Fatal("krb5 认证失败！", err)
	}

	// 启动 gin 代理服务
	if config.C.Log.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()

	engine.Use(gin.Recovery())
	engine.Use(ginLogger())

	if config.C.Server.EnablePprof {
		pprof.Register(engine)
	}

	if config.C.Server.EnablePrometheus {
		p := ginprometheus.NewPrometheus("gin")
		p.Use(engine)
	}

	engine.NoRoute(func(ctx *gin.Context) {
		path := ctx.Request.URL.Path
		logrus.Debugf("收到无匹配路由 %s ,开始尝试认证代理！", path)
		if !ctx.IsAborted() {
			proxy.DoSpnego(ctx)
		}
	})

	engine.Run(fmt.Sprintf("0.0.0.0:%d", config.C.Server.Port))
}

func ginLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Stop timer
		stop := time.Now()
		latency := stop.Sub(start)

		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		bodySize := c.Writer.Size()

		if raw != "" {
			path = path + "?" + raw
		}

		if statusCode >= 400 {
			logrus.Errorf("gin request: %s - [%s] \"%s %s %s %d %s \"%s\" %d\"\n",
				clientIP,
				stop.Format(time.RFC1123),
				method,
				path,
				c.Request.Proto,
				statusCode,
				latency,
				c.Request.UserAgent(),
				bodySize,
			)
		} else {
			logrus.Debugf("gin request: %s - [%s] \"%s %s %s %d %s \"%s\" %d\"\n",
				clientIP,
				stop.Format(time.RFC1123),
				method,
				path,
				c.Request.Proto,
				statusCode,
				latency,
				c.Request.UserAgent(),
				bodySize,
			)
		}
	}
}
