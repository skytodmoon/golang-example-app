package oauth

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/aristat/golang-gin-oauth2-example-app/app/logger"

	"github.com/go-oauth2/oauth2/models"

	"github.com/go-session/session"
	"gopkg.in/oauth2.v3"
	"gopkg.in/oauth2.v3/errors"
	"gopkg.in/oauth2.v3/manage"
	"gopkg.in/oauth2.v3/server"
	"gopkg.in/oauth2.v3/store"

	oauthRedis "gopkg.in/go-oauth2/redis.v3"
)

const prefix = "app.oauth"

// OAuth
type OAuth struct {
	ctx          context.Context
	cfg          Config
	log          *logger.Zap
	OauthServer  OauthServer
	OauthService *Service
}

var ClientsConfig = map[string]oauth2.ClientInfo{
	"123456": &models.Client{
		ID:     "123456",
		Secret: "12345678",
		Domain: "http://localhost:9094",
	},
}

type Oauth2Service struct {
	SessionManager *session.Manager
	TokenStore     oauth2.TokenStore
	ClientStore    *store.ClientStore
}

type OauthServer interface {
	HandleAuthorizeRequest(w http.ResponseWriter, r *http.Request) (err error)
	HandleTokenRequest(w http.ResponseWriter, r *http.Request) (err error)
	ValidationBearerToken(r *http.Request) (ti oauth2.TokenInfo, err error)
}

type oauth2Server struct {
	*server.Server
}

func NewClientStore(config map[string]oauth2.ClientInfo) *store.ClientStore {
	clientStore := store.NewClientStore()
	for key, value := range config {
		clientStore.Set(key, value)
	}

	return clientStore
}

func NewOauthServer(oauth2Service *Oauth2Service, log *logger.Zap) OauthServer {
	manager := manage.NewDefaultManager()
	manager.SetAuthorizeCodeTokenCfg(
		&manage.Config{
			AccessTokenExp:    time.Hour * 24 * 7,
			RefreshTokenExp:   time.Hour * 24 * 14,
			IsGenerateRefresh: true,
		},
	)

	manager.MapTokenStorage(oauth2Service.TokenStore)
	manager.MapClientStorage(oauth2Service.ClientStore)

	server := server.NewDefaultServer(manager)
	server.UserAuthorizationHandler = userAuthorization(oauth2Service)
	server.SetInternalErrorHandler(func(err error) (re *errors.Response) {
		log.Error("Internal Error: %s", logger.Args(err.Error()))
		return
	})
	server.SetResponseErrorHandler(func(re *errors.Response) {
		log.Error("Response Error: %s", logger.Args(re.Error.Error()))
	})

	return NewOauthServerWithServer(server)
}

// New
func New(ctx context.Context, log *logger.Zap, cfg Config, session *session.Manager) *OAuth {
	log.Info("Initialize oauth")

	oauthConfig := oauthRedis.Options{
		Addr: cfg.RedisUrl,
		DB:   cfg.RedisDB,
	}

	oauth2Service := &Oauth2Service{
		TokenStore:     oauthRedis.NewRedisStore(&oauthConfig),
		ClientStore:    NewClientStore(ClientsConfig),
		SessionManager: session,
	}

	oauthServer := NewOauthServer(oauth2Service, log)
	authService := &Service{
		SessionManager: session,
		OauthServer:    oauthServer,
	}

	return &OAuth{
		ctx:          ctx,
		cfg:          cfg,
		log:          log.WithFields(logger.Fields{"service": prefix}),
		OauthServer:  oauthServer,
		OauthService: authService,
	}
}

func NewOauthServerWithServer(srv *server.Server) OauthServer {
	return &oauth2Server{Server: srv}
}

func userAuthorization(service *Oauth2Service) func(w http.ResponseWriter, r *http.Request) (userID string, err error) {
	return func(w http.ResponseWriter, r *http.Request) (userID string, err error) {
		log.Printf("[INFO] userAuthorization %s", r.URL)
		sessionStore, err := service.SessionManager.Start(context.Background(), w, r)
		if err != nil {
			return
		}

		uid, ok := sessionStore.Get("LoggedInUserID")
		if !ok {
			if r.Form == nil {
				r.ParseForm()
			}

			sessionStore.Set("ReturnUri", r.Form.Encode())
			sessionStore.Save()

			w.Header().Set("Location", "/login")
			w.WriteHeader(http.StatusFound)
			return
		}
		userID = uid.(string)

		// Authorization for receiving a token
		sessionStore.Delete("LoggedInUserID")
		sessionStore.Save()

		return
	}
}
