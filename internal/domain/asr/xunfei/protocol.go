package xunfei

type request struct {
	Common   common   `json:"common,omitempty"`
	Business business `json:"business,omitempty"`
	Data     data     `json:"data"`
}

type common struct {
	AppID string `json:"app_id"`
}

type business struct {
	Language string `json:"language"`
	Domain   string `json:"domain"`
	Accent   string `json:"accent,omitempty"`
}

type data struct {
	Status   int    `json:"status"`
	Format   string `json:"format"`
	Audio    string `json:"audio"`
	Encoding string `json:"encoding"`
}

type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	SID     string `json:"sid"`
	Data    struct {
		Status int `json:"status"`
		Result struct {
			Ws []struct {
				Cw []struct {
					W string `json:"w"`
				} `json:"cw"`
			} `json:"ws"`
		} `json:"result"`
	} `json:"data"`
}

func (r *response) extractText() string {
	text := ""
	for _, ws := range r.Data.Result.Ws {
		for _, cw := range ws.Cw {
			text += cw.W
			break
		}
	}
	return text
}
