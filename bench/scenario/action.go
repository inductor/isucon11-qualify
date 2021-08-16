package scenario

// action.go
// 1. リクエストを投げ
// 2. レスポンスを受け取り
// 3. ステータスコードを検証し
// 4. レスポンスをstructにマッピング
// 5. not nullのはずのフィールドのNULL値チェック
// までを行う
//
// 間で都度エラーチェックをする
// エラーが出た場合は返り値に入る

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sync"
	"time"

	"github.com/isucon/isucandar/agent"
	"github.com/isucon/isucandar/failure"
	"github.com/isucon/isucon11-qualify/bench/logger"
	"github.com/isucon/isucon11-qualify/bench/model"
	"github.com/isucon/isucon11-qualify/bench/random"
	"github.com/isucon/isucon11-qualify/bench/service"
)

//Action

// ==============================initialize==============================

func initializeAction(ctx context.Context, a *agent.Agent, req service.PostInitializeRequest) (*service.InitializeResponse, []error) {
	errors := []error{}

	//リクエスト
	body, err := json.Marshal(req)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	initializeResponse := &service.InitializeResponse{}
	res, err := reqJSONResJSON(ctx, a, http.MethodPost, "/initialize", bytes.NewReader(body), &initializeResponse, []int{http.StatusOK})
	if err != nil {
		errors = append(errors, err)
	} else {
		//データの検証
		if initializeResponse.Language == "" {
			err = errorBadResponse(res, "利用言語(language)が設定されていません")
			errors = append(errors, err)
		}
	}

	return initializeResponse, errors
}

// ==============================authAction==============================

func authAction(ctx context.Context, user *model.User, userID string) (*service.AuthResponse, []error) {
	errors := []error{}

	//JWT生成
	jwtOK, err := service.GenerateJWT(userID, time.Now())
	if err != nil {
		logger.AdminLogger.Panic(err)
	}

	// リダイレクト時のフロントアクセス
	if errs := BrowserAccessIndexHtml(ctx, user, "/"); len(errs) != 0 {
		errors = append(errors, errs...)
		return nil, errors
	}

	//リクエスト
	req, err := user.GetAgent().POST("/api/auth", nil)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	req.Header.Set("Authorization", jwtOK)
	res, err := AgentDo(user.GetAgent(), ctx, req)
	if err != nil {
		err = failure.NewError(ErrHTTP, err)
		errors = append(errors, err)
		return nil, errors
	}
	defer res.Body.Close()

	//httpリクエストの検証
	authResponse := &service.AuthResponse{}
	err = verifyStatusCode(res, http.StatusOK)
	if err != nil {
		errors = append(errors, err)
		return nil, errors
	}

	//データの検証
	//NoContentなので無し

	//Cookie検証
	found := false
	for _, c := range res.Cookies() {
		if c.Name == "isucondition" {
			found = true
			break
		}
	}
	if !found {
		err = errorBadResponse(res, "cookieがありません")
		errors = append(errors, err)
	}

	return authResponse, errors
}

func authActionOnlyApi(ctx context.Context, a *agent.Agent, userID string) (*service.AuthResponse, []error) {
	errors := []error{}

	//JWT生成
	jwtOK, err := service.GenerateJWT(userID, time.Now())
	if err != nil {
		logger.AdminLogger.Panic(err)
	}

	//リクエスト
	req, err := a.POST("/api/auth", nil)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	req.Header.Set("Authorization", jwtOK)
	res, err := AgentDo(a, ctx, req)
	if err != nil {
		err = failure.NewError(ErrHTTP, err)
		errors = append(errors, err)
		return nil, errors
	}
	defer res.Body.Close()

	//httpリクエストの検証
	authResponse := &service.AuthResponse{}
	err = verifyStatusCode(res, http.StatusOK)
	if err != nil {
		errors = append(errors, err)
		return nil, errors
	}

	//データの検証
	//NoContentなので無し

	//Cookie検証
	found := false
	for _, c := range res.Cookies() {
		if c.Name == "isucondition" {
			found = true
			break
		}
	}
	if !found {
		err = errorBadResponse(res, "cookieがありません")
		errors = append(errors, err)
	}

	return authResponse, errors
}

func authActionWithInvalidJWT(ctx context.Context, a *agent.Agent, invalidJWT string, expectedCode int, expectedBody string) []error {
	errors := []error{}

	//リクエスト
	req, err := a.POST("/api/auth", nil)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	req.Header.Set("Authorization", invalidJWT)
	res, err := AgentDo(a, ctx, req)
	if err != nil {
		err = failure.NewError(ErrHTTP, err)
		errors = append(errors, err)
		return errors
	}
	defer res.Body.Close()

	//httpリクエストの検証
	err = verifyStatusCode(res, expectedCode)
	if err != nil {
		errors = append(errors, err)
		return errors
	}

	//データの検証
	responseBody, _ := ioutil.ReadAll(res.Body)
	if string(responseBody) != expectedBody {
		err = errorMismatch(res, "エラーメッセージが不正確です: `%s` (expected: `%s`)", string(responseBody), expectedBody)
		errors = append(errors, err)
	}

	return errors
}

func authActionWithoutJWT(ctx context.Context, a *agent.Agent) []error {
	errors := []error{}

	//リクエスト
	req, err := a.POST("/api/auth", nil)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	res, err := AgentDo(a, ctx, req)
	if err != nil {
		err = failure.NewError(ErrHTTP, err)
		errors = append(errors, err)
		return errors
	}
	defer res.Body.Close()

	//httpリクエストの検証
	err = verifyStatusCode(res, http.StatusForbidden)
	if err != nil {
		errors = append(errors, err)
		return errors
	}

	//データの検証
	const expectedBody = "forbidden"
	responseBody, _ := ioutil.ReadAll(res.Body)
	if string(responseBody) != expectedBody {
		err = errorMismatch(res, "エラーメッセージが不正確です: `%s` (expected: `%s`)", string(responseBody), expectedBody)
		errors = append(errors, err)
	}

	return errors
}

//auth utility

const authActionErrorNum = 8 //authActionErrorが何種類のエラーを持っているか

//正しく失敗するか確認するAction
func authActionError(ctx context.Context, agt *agent.Agent, userID string, errorType int) []error {
	switch errorType % authActionErrorNum {
	case 0:
		//Unexpected signing method, StatusForbidden
		jwtHS256, err := service.GenerateHS256JWT(userID, time.Now())
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		return authActionWithForbiddenJWT(ctx, agt, jwtHS256)
	case 1:
		//expired, StatusForbidden
		jwtExpired, err := service.GenerateJWT(userID, time.Now().Add(-365*24*time.Hour))
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		return authActionWithForbiddenJWT(ctx, agt, jwtExpired)
	case 2:
		//jwt is missing, StatusForbidden
		return authActionWithoutJWT(ctx, agt)
	case 3:
		//invalid private key, StatusForbidden
		jwtDummyKey, err := service.GenerateDummyJWT(userID, time.Now())
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		return authActionWithForbiddenJWT(ctx, agt, jwtDummyKey)
	case 4:
		//not jwt, StatusForbidden
		return authActionWithForbiddenJWT(ctx, agt, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.")
	case 5:
		//偽装されたjwt, StatusForbidden
		userID2 := random.UserName()
		jwtTampered, err := service.GenerateTamperedJWT(userID, userID2, time.Now())
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		return authActionWithForbiddenJWT(ctx, agt, jwtTampered)
	case 6:
		//jia_user_id is missing, StatusBadRequest
		jwtNoData, err := service.GenerateJWTWithNoData(time.Now())
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		return authActionWithInvalidJWT(ctx, agt, jwtNoData, http.StatusBadRequest, "invalid JWT payload")
	case 7:
		//jwt with invalid data type, StatusBadRequest
		jwtInvalidDataType, err := service.GenerateJWTWithInvalidType(userID, time.Now())
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		return authActionWithInvalidJWT(ctx, agt, jwtInvalidDataType, http.StatusBadRequest, "invalid JWT payload")
	}

	//ロジック的に到達しないはずだけど念のためエラー処理
	err := fmt.Errorf("internal bench error @authActionError: errorType=%v", errorType)
	logger.AdminLogger.Panic(err)
	return []error{failure.NewError(ErrCritical, err)}
}

func authActionWithForbiddenJWT(ctx context.Context, a *agent.Agent, invalidJWT string) []error {
	return authActionWithInvalidJWT(ctx, a, invalidJWT, http.StatusForbidden, "forbidden")
}

func signoutAction(ctx context.Context, a *agent.Agent) (*http.Response, error) {
	res, err := reqNoContentResNoContent(ctx, a, http.MethodPost, "/api/signout", []int{http.StatusOK})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func signoutErrorAction(ctx context.Context, a *agent.Agent) (string, *http.Response, error) {
	res, resBody, err := reqNoContentResError(ctx, a, http.MethodPost, "/api/signout", []int{http.StatusUnauthorized})
	if err != nil {
		return resBody, nil, err
	}
	return resBody, res, nil
}

func getMeAction(ctx context.Context, a *agent.Agent) (*service.GetMeResponse, *http.Response, error) {
	me := &service.GetMeResponse{}
	res, err := reqJSONResJSON(ctx, a, http.MethodGet, "/api/user/me", nil, me, []int{http.StatusOK})
	if err != nil {
		return nil, nil, err
	}
	return me, res, nil
}

func getMeErrorAction(ctx context.Context, a *agent.Agent) (string, *http.Response, error) {
	res, resBody, err := reqJSONResError(ctx, a, http.MethodGet, "/api/user/me", nil, []int{http.StatusUnauthorized})
	if err != nil {
		return resBody, nil, err
	}
	return resBody, res, nil
}

func getIsuAction(ctx context.Context, a *agent.Agent) ([]*service.Isu, *http.Response, error) {
	targetURL, err := url.Parse("/api/isu")
	if err != nil {
		logger.AdminLogger.Panicln(err)
	}

	var isuList []*service.Isu
	res, err := reqJSONResJSON(ctx, a, http.MethodGet, targetURL.String(), nil, &isuList, []int{http.StatusOK})
	if err != nil {
		return nil, nil, err
	}
	return isuList, res, nil
}

func getIsuErrorAction(ctx context.Context, a *agent.Agent) (string, *http.Response, error) {
	targetURL, err := url.Parse("/api/isu")
	if err != nil {
		logger.AdminLogger.Panicln(err)
	}

	res, resBody, err := reqJSONResError(ctx, a, http.MethodGet, targetURL.String(), nil, []int{http.StatusUnauthorized, http.StatusBadRequest})
	if err != nil {
		return "", nil, err
	}
	return resBody, res, nil
}

func getPathWithParams(pathStr string, query url.Values) string {
	path, err := url.Parse(pathStr)
	if err != nil {
		logger.AdminLogger.Panicln(err)
	}

	path.RawQuery = query.Encode()
	return path.String()
}

func postIsuAction(ctx context.Context, a *agent.Agent, req service.PostIsuRequest) (*service.Isu, *http.Response, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	part, err := writer.CreateFormField("jia_isu_uuid")
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	_, err = part.Write([]byte(req.JIAIsuUUID))
	if err != nil {
		logger.AdminLogger.Panic(err)
	}

	part, err = writer.CreateFormField("isu_name")
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	_, err = part.Write([]byte(req.IsuName))
	if err != nil {
		logger.AdminLogger.Panic(err)
	}

	if req.Img != nil {
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Type", "image/jpeg")
		partHeader.Set("Content-Disposition", `form-data; name="image"; filename="image.jpeg"`)

		part, err := writer.CreatePart(partHeader)
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		_, err = part.Write(req.Img)
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
	}

	err = writer.Close()
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	isu := &service.Isu{}
	res, err := reqMultipartResJSON(ctx, a, http.MethodPost, "/api/isu", buf, writer, isu, []int{http.StatusCreated})
	if err != nil {
		return nil, res, err
	}
	return isu, res, nil
}

func postIsuErrorAction(ctx context.Context, a *agent.Agent, req service.PostIsuRequest) (string, *http.Response, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	part, err := writer.CreateFormField("jia_isu_uuid")
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	_, err = part.Write([]byte(req.JIAIsuUUID))
	if err != nil {
		logger.AdminLogger.Panic(err)
	}

	part, err = writer.CreateFormField("isu_name")
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	_, err = part.Write([]byte(req.IsuName))
	if err != nil {
		logger.AdminLogger.Panic(err)
	}

	if req.Img != nil {
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Type", "image/jpeg")
		partHeader.Set("Content-Disposition", `form-data; name="image"; filename="image.jpeg"`)

		part, err := writer.CreatePart(partHeader)
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
		_, err = part.Write(req.Img)
		if err != nil {
			logger.AdminLogger.Panic(err)
		}
	}

	err = writer.Close()
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	res, text, err := reqMultipartResError(ctx, a, http.MethodPost, "/api/isu", buf, writer, []int{http.StatusBadRequest, http.StatusConflict, http.StatusUnauthorized, http.StatusNotFound, http.StatusForbidden})
	if err != nil {
		return "", res, err
	}
	return text, res, nil
}

func getIsuIdAction(ctx context.Context, a *agent.Agent, id string) (*service.Isu, *http.Response, error) {
	isu := &service.Isu{}
	reqUrl := fmt.Sprintf("/api/isu/%s", id)
	res, err := reqJSONResJSON(ctx, a, http.MethodGet, reqUrl, nil, &isu, []int{http.StatusOK})
	if err != nil {
		return nil, nil, err
	}
	return isu, res, nil
}

func getIsuIdErrorAction(ctx context.Context, a *agent.Agent, id string) (string, *http.Response, error) {
	reqUrl := fmt.Sprintf("/api/isu/%s", id)
	res, text, err := reqNoContentResError(ctx, a, http.MethodGet, reqUrl, []int{http.StatusUnauthorized, http.StatusNotFound})
	if err != nil {
		return "", nil, err
	}
	return text, res, nil
}

func getIsuIconAction(ctx context.Context, a *agent.Agent, id string) ([]byte, *http.Response, error) {
	reqUrl := fmt.Sprintf("/api/isu/%s/icon", id)
	allowedStatusCodes := []int{http.StatusOK}
	res, image, err := reqNoContentResPng(ctx, a, http.MethodGet, reqUrl, allowedStatusCodes)
	if err != nil {
		return nil, nil, err
	}
	return image, res, nil
}

func getIsuIconErrorAction(ctx context.Context, a *agent.Agent, id string) (string, *http.Response, error) {
	reqUrl := fmt.Sprintf("/api/isu/%s/icon", id)
	res, text, err := reqNoContentResError(ctx, a, http.MethodGet, reqUrl, []int{http.StatusUnauthorized, http.StatusNotFound})
	if err != nil {
		return "", nil, err
	}
	return text, res, nil
}

func postIsuConditionAction(ctx context.Context, httpClient http.Client, targetUrl string, req *[]service.PostIsuConditionRequest) (*http.Response, error) {
	conditionByte, err := json.Marshal(req)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", targetUrl, bytes.NewBuffer(conditionByte))
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "JIA-Members-Client/1.2")
	res, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return res, nil
}

func postIsuConditionErrorAction(ctx context.Context, httpClient http.Client, targetUrl string, req []map[string]interface{}) (string, *http.Response, error) {
	conditionByte, err := json.Marshal(req)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", targetUrl, bytes.NewBuffer(conditionByte))
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "JIA-Members-Client/1.2")
	res, err := httpClient.Do(httpReq)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()

	resBody, err := checkContentTypeAndGetBody(res, "text/plain")
	if err != nil {
		return "", nil, err
	}

	return string(resBody), res, nil
}

func getIsuConditionAction(ctx context.Context, a *agent.Agent, id string, req service.GetIsuConditionRequest) ([]*service.GetIsuConditionResponse, *http.Response, error) {
	reqUrl := getIsuConditionRequestParams(fmt.Sprintf("/api/condition/%s", id), req)
	conditions := []*service.GetIsuConditionResponse{}
	res, err := reqJSONResJSON(ctx, a, http.MethodGet, reqUrl, nil, &conditions, []int{http.StatusOK})
	if err != nil {
		return nil, nil, err
	}
	return conditions, res, nil
}

func getIsuConditionErrorAction(ctx context.Context, a *agent.Agent, id string, query url.Values) (string, *http.Response, error) {
	path := fmt.Sprintf("/api/condition/%s", id)
	rpath := getPathWithParams(path, query)
	res, text, err := reqNoContentResError(ctx, a, http.MethodGet, rpath, []int{http.StatusNotFound, http.StatusUnauthorized, http.StatusBadRequest})
	if err != nil {
		return "", nil, err
	}
	return text, res, nil
}

func getIsuConditionRequestParams(base string, req service.GetIsuConditionRequest) string {
	targetURL, err := url.Parse(base)
	if err != nil {
		logger.AdminLogger.Panicln(err)
	}

	q := url.Values{}
	if req.StartTime != nil {
		q.Set("start_time", fmt.Sprint(*req.StartTime))
	}
	q.Set("end_time", fmt.Sprint(req.EndTime))
	q.Set("condition_level", req.ConditionLevel)
	targetURL.RawQuery = q.Encode()
	return targetURL.String()
}

func getIsuGraphAction(ctx context.Context, a *agent.Agent, id string, req service.GetGraphRequest) (service.GraphResponse, *http.Response, error) {
	graph := service.GraphResponse{}
	reqUrl := fmt.Sprintf("/api/isu/%s/graph?datetime=%d", id, req.Date)
	res, err := reqJSONResJSON(ctx, a, http.MethodGet, reqUrl, nil, &graph, []int{http.StatusOK})
	if err != nil {
		return nil, nil, err
	}

	return graph, res, nil
}

func getIsuGraphErrorAction(ctx context.Context, a *agent.Agent, id string, query url.Values) (string, *http.Response, error) {
	path := fmt.Sprintf("/api/isu/%s/graph", id)
	rpath := getPathWithParams(path, query)
	res, text, err := reqNoContentResError(ctx, a, http.MethodGet, rpath, []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusBadRequest})
	if err != nil {
		return "", nil, err
	}
	return text, res, nil
}

func getTrendAction(ctx context.Context, a *agent.Agent) (service.GetTrendResponse, *http.Response, error) {
	reqUrl := "/api/trend"
	trend, res, err := reqJSONResTrend(ctx, a, http.MethodGet, reqUrl, nil, []int{http.StatusOK})
	if err != nil {
		return nil, nil, err
	}

	return trend, res, nil
}

func browserGetLandingPageAction(ctx context.Context, user AgentWithStaticCache) (service.GetTrendResponse, *http.Response, []error) {
	// 静的ファイルのGET
	if err := BrowserAccess(ctx, user, "/", TrendPage); err != nil {
		return nil, nil, err
	}

	trend, res, err := getTrendAction(ctx, user.GetAgent())
	if err != nil {
		return nil, nil, []error{err}
	}

	return trend, res, nil
}

func browserGetLandingPageIgnoreAction(ctx context.Context, user AgentWithStaticCache) []error {
	// 静的ファイルのGET
	if err := BrowserAccess(ctx, user, "/", TrendPage); err != nil {
		return err
	}

	_, err := getTrendIgnoreAction(ctx, user.GetAgent())
	if err != nil {
		// ここのエラーは気にしないので握りつぶす
		return nil
	}

	return nil
}

func getTrendIgnoreAction(ctx context.Context, a *agent.Agent) (*http.Response, error) {
	reqUrl := "/api/trend"
	res, err := reqJSONResNoContent(ctx, a, http.MethodGet, reqUrl, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func browserGetHomeAction(ctx context.Context, a *agent.Agent,
	validateIsu func(*http.Response, []*service.Isu) []error,
) ([]*service.Isu, []error) {
	errors := []error{}
	isuList, hres, err := getIsuAction(ctx, a)
	if err != nil {
		errors = append(errors, err)
	}
	if isuList != nil {
		var wg sync.WaitGroup
		var errMutex sync.Mutex
		wg.Add(len(isuList))
		for _, isu := range isuList {
			go func(isu *service.Isu) {
				defer wg.Done()
				icon, res, err := getIsuIconAction(ctx, a, isu.JIAIsuUUID)
				if err != nil {
					isu.Icon = nil
					errMutex.Lock()
					errors = append(errors, err)
					errMutex.Unlock()
				} else {
					isu.Icon = icon
					isu.IconStatusCode = res.StatusCode
				}
			}(isu)
		}
		wg.Wait()
		errors = append(errors, validateIsu(hres, isuList)...)
	}

	return isuList, errors
}

func browserGetIsuDetailAction(ctx context.Context, a *agent.Agent, id string,
	validateIsu func(*http.Response, *service.Isu) []error,
) (*service.Isu, []error) {

	errors := []error{}
	isu, res, err := getIsuIdAction(ctx, a, id)
	if err != nil {
		errors = append(errors, err)
		return nil, errors
	}
	if isu != nil {
		icon, resIcon, err := getIsuIconAction(ctx, a, id)
		if err != nil {
			isu.Icon = nil
			errors = append(errors, err)
		} else {
			isu.Icon = icon
			isu.IconStatusCode = resIcon.StatusCode
		}

		errors = append(errors, validateIsu(res, isu)...)
	}
	return isu, errors
}

func browserGetIsuConditionAction(ctx context.Context, a *agent.Agent, id string, req service.GetIsuConditionRequest,
	validateCondition func(*http.Response, []*service.GetIsuConditionResponse) []error,
) ([]*service.GetIsuConditionResponse, []error) {
	errors := []error{}
	conditions, hres, err := getIsuConditionAction(ctx, a, id, req)
	if err != nil {
		errors = append(errors, err)
	} else {
		errors = append(errors, validateCondition(hres, conditions)...)
	}
	return conditions, errors
}

func BrowserAccessIndexHtml(ctx context.Context, user AgentWithStaticCache, rpath string) []error {
	req, err := user.GetAgent().GET(rpath)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	res, err := AgentStaticDo(ctx, user, req, "/index.html")
	if err != nil {
		return []error{failure.NewError(ErrHTTP, err)}
	}
	// index.html の hash 検証
	if err := errorAssetChecksum(req, res, user, "/index.html"); err != nil {
		return []error{err}
	}

	return nil
}

func BrowserAccess(ctx context.Context, user AgentWithStaticCache, rpath string, page PageType) []error {
	req, err := user.GetAgent().GET(rpath)
	if err != nil {
		logger.AdminLogger.Panic(err)
	}
	res, err := AgentStaticDo(ctx, user, req, "/index.html")
	if err != nil {
		return []error{failure.NewError(ErrHTTP, err)}
	}
	// index.html の hash 検証
	err = errorAssetChecksum(req, res, user, "/index.html")
	if err != nil {
		return []error{err}
	}

	// index.html で取りに行っているはずの assets の取得
	errs := getAssets(ctx, user, res, page)
	if len(errs) != 0 {
		return errs
	}

	return nil
}

func AgentDo(a *agent.Agent, ctx context.Context, req *http.Request) (*http.Response, error) {
	return a.Do(ctx, req)
}

type AgentWithStaticCache interface {
	SetStaticCache(path string, hash [16]byte)
	GetStaticCache(path string, req *http.Request) ([16]byte, bool)

	GetAgent() *agent.Agent
}

// User.StaticCachedHash を使って静的ファイルのキャッシュを更新する
func AgentStaticDo(ctx context.Context, user AgentWithStaticCache, req *http.Request, cachePath string) (*http.Response, error) {
	res, err := user.GetAgent().Do(ctx, req)
	if err != nil {
		return res, err
	}

	return res, nil
}

func getAssets(ctx context.Context, user AgentWithStaticCache, resIndex *http.Response, page PageType) []error {
	errs := []error{}

	faviconSvg := resourcesMap["/assets/favicon.svg"]
	indexCss := resourcesMap["/assets/index.css"]
	indexJs := resourcesMap["/assets/index.js"]
	//logoOrange := resourcesMap["/assets/logo_orange.svg"]
	//logoWhite := resourcesMap["/assets/logo_white.svg"]
	vendorJs := resourcesMap["/assets/vendor.js"]

	var requireAssets []string
	switch page {
	case HomePage, IsuDetailPage, IsuConditionPage, IsuGraphPage, RegisterPage:
		requireAssets = []string{faviconSvg, indexCss, vendorJs, indexJs /*logoWhite,*/}
	case TrendPage:
		requireAssets = []string{faviconSvg, indexCss, vendorJs, indexJs /*logoOrange, logoWhite,*/, vendorJs}
	default:
		logger.AdminLogger.Panicf("意図していないpage(%d)のResourceCheckを行っています。(path: %s)", page, resIndex.Request.URL.Path)
	}

	wg := &sync.WaitGroup{}
	errsMx := sync.Mutex{}
	for _, asset := range requireAssets {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			rpath := joinURL(resIndex.Request.URL, path)
			req, err := user.GetAgent().GET(rpath)
			if err != nil {
				logger.AdminLogger.Panic(err)
			}
			res, err := AgentStaticDo(ctx, user, req, path)
			if err != nil {
				errsMx.Lock()
				errs = append(errs, failure.NewError(ErrHTTP, err))
				errsMx.Unlock()
				return
			}
			err = errorAssetChecksum(req, res, user, path)
			if err != nil {
				errsMx.Lock()
				errs = append(errs, err)
				errsMx.Unlock()
			}
		}(asset)
	}
	wg.Wait()
	return errs
}
