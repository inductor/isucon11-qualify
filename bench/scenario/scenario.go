package scenario

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/isucon/isucandar"
	"github.com/isucon/isucandar/agent"
	"github.com/isucon/isucon11-qualify/bench/logger"
	"github.com/isucon/isucon11-qualify/bench/model"
	"github.com/isucon/isucon11-qualify/bench/service"
)

// scenario.go
// シナリオ構造体とそのメンバ関数
// および、全ステップで使うシナリオ関数

type Scenario struct {
	// TODO: シナリオ実行に必要なフィールドを書く

	BaseURL          string // ベンチ対象 Web アプリの URL
	UseTLS           bool   // https で接続するかどうか
	NoLoad           bool   // Load(ベンチ負荷)を強要しない
	realTimeStart    time.Time
	virtualTimeStart time.Time
	virtualTimeMulti time.Duration //時間が何倍速になっているか
	jiaServiceURL    string

	// POST /initialize の猶予時間
	initializeTimeout time.Duration

	// 競技者の実装言語
	Language string

	loadWaitGroup                  sync.WaitGroup
	jiaChancel                     context.CancelFunc
	ScenarioNormalUserDeleteCount  int32 //TODO: localにする
	ScenarioCompanyUserDeleteCount int32 //TODO: localにする

	//内部状態
	normalUsersMtx  sync.Mutex
	normalUsers     []*model.User
	companyUsersMtx sync.Mutex
	companyUsers    []*model.User
	Catalogs        map[string]*model.IsuCatalog
}

func NewScenario(jiaServiceURL string) (*Scenario, error) {
	return &Scenario{
		// TODO: シナリオを初期化する
		//realTimeStart: time.Now()
		virtualTimeStart:  time.Date(2020, 7, 1, 0, 0, 0, 0, time.Local), //TODO: ちゃんと決める
		virtualTimeMulti:  3000,                                          //5分=300秒に一回 => 1秒に10回
		jiaServiceURL:     jiaServiceURL,
		initializeTimeout: 20 * time.Second,
		normalUsers:       []*model.User{},
		companyUsers:      []*model.User{},
	}, nil
}

func (s *Scenario) WithInitializeTimeout(t time.Duration) *Scenario {
	s.initializeTimeout = t
	return s
}

func (s *Scenario) NewAgent(opts ...agent.AgentOption) (*agent.Agent, error) {
	opts = append(opts, agent.WithBaseURL(s.BaseURL))
	return agent.NewAgent(opts...)
}

func (s *Scenario) ToVirtualTime(realTime time.Time) time.Time {
	return s.virtualTimeStart.Add(realTime.Sub(s.realTimeStart) * s.virtualTimeMulti)
}

var countUsers int32

func init() {
	go func() {
		for {
			// 途中経過を3秒毎に送信
			timer := time.After(3 * time.Second)
			//sendResult(s, step.Result(), false)//TODO:

			<-timer
			logger.AdminLogger.Printf("countUsers: %v\n", atomic.LoadInt32(&countUsers))
		}

	}()
}

//load用
//通常ユーザーのシナリオスレッドを追加する
func (s *Scenario) AddNormalUser(ctx context.Context, step *isucandar.BenchmarkStep, count int) {
	if count <= 0 {
		return
	}
	s.loadWaitGroup.Add(int(count))
	for i := 0; i < count; i++ {
		atomic.AddInt32(&countUsers, +1)
		go func(ctx context.Context, step *isucandar.BenchmarkStep) {
			defer s.loadWaitGroup.Done()
			defer atomic.AddInt32(&countUsers, -1)
			s.loadNormalUser(ctx, step)
		}(ctx, step)
	}
}

//load用
//マニアユーザーのシナリオスレッドを追加する
func (s *Scenario) AddManiacUser(ctx context.Context, step *isucandar.BenchmarkStep, count int) {
	if count <= 0 {
		return
	}
	s.loadWaitGroup.Add(int(count))
	for i := 0; i < count; i++ {
		atomic.AddInt32(&countUsers, +1)
		go func(ctx context.Context, step *isucandar.BenchmarkStep) {
			defer s.loadWaitGroup.Done()
			defer atomic.AddInt32(&countUsers, -1)
			//s.loadManiacUser(ctx, step) //TODO:
		}(ctx, step)
	}
}

//load用
//企業ユーザーのシナリオスレッドを追加する
func (s *Scenario) AddCompanyUser(ctx context.Context, step *isucandar.BenchmarkStep, count int) {
	if count <= 0 {
		return
	}
	s.loadWaitGroup.Add(int(count))
	for i := 0; i < count; i++ {
		atomic.AddInt32(&countUsers, +1)
		go func(ctx context.Context, step *isucandar.BenchmarkStep) {
			defer s.loadWaitGroup.Done()
			defer atomic.AddInt32(&countUsers, -1)
			s.loadCompanyUser(ctx, step)
		}(ctx, step)
	}
}

//新しい登録済みUserの生成
//失敗したらnilを返す
func (s *Scenario) NewUser(ctx context.Context, step *isucandar.BenchmarkStep, a *agent.Agent, userType model.UserType) *model.User {
	user, err := model.NewRandomUserRaw(userType)
	if err != nil {
		logger.AdminLogger.Panic(err)
		return nil
	}

	//backendにpostする
	//TODO: 確率で失敗してリトライする
	_, errs := authAction(ctx, a, user.UserID)
	for _, err := range errs {
		step.AddError(err)
	}
	if len(errs) > 0 {
		return nil
	}
	user.Agent = a

	return user
}

//新しい登録済みISUの生成
//失敗したらnilを返す
func (s *Scenario) NewIsu(ctx context.Context, step *isucandar.BenchmarkStep, owner *model.User, addToUser bool) *model.Isu {
	isu, streamsForPoster, err := model.NewRandomIsuRaw(owner)
	if err != nil {
		logger.AdminLogger.Panic(err)
		return nil
	}

	//ISU協会にIsu*を登録する必要あり
	RegisterToJiaAPI(isu.JIAIsuUUID, &IsuDetailInfomation{CatalogID: isu.JIACatalogID, Character: isu.Character}, streamsForPoster)

	//backendにpostする
	//TODO: 確率で失敗してリトライする
	req := service.PostIsuRequest{
		JIAIsuUUID: isu.JIAIsuUUID,
		IsuName:    isu.Name,
	}
	isuResponse, res, err := postIsuAction(ctx, owner.Agent, req)
	if err != nil {
		step.AddError(err)
		isu.StreamsForScenario.StateChan <- model.IsuStateChangeDelete
		return nil
	}
	if isuResponse.JIAIsuUUID != isu.JIAIsuUUID ||
		isuResponse.Name != isu.Name ||
		isuResponse.JIACatalogID != isu.JIACatalogID ||
		isuResponse.Character != isu.Character {
		step.AddError(errorMissmatch(res, "レスポンスBodyが正しくありません"))
	}
	isu.StreamsForScenario.StateChan <- model.IsuStateChangeNone

	//並列に生成する場合は後でgetにより正しい順番を得て、その順序でaddする
	//その場合はaddToUser==falseになる
	if addToUser {
		//戻り値をownerに追加する
		owner.AddIsu(isu)
	}

	return isu
}

func GetConditionDataExistTimestamp(s *Scenario, user *model.User) int64 {
	if len(user.IsuListOrderByCreatedAt) == 0 {
		return s.virtualTimeStart.Unix()
	}
	var timestamp int64 = math.MaxInt64
	for _, isu := range user.IsuListOrderByCreatedAt {
		cond := isu.Conditions.Back()
		if cond == nil {
			return s.virtualTimeStart.Unix()
		}
		if cond.TimestampUnix < timestamp {
			timestamp = cond.TimestampUnix
		}
	}
	return timestamp
}
