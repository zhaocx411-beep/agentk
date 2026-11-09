package modelcatalog

import (
	"agentk/internal/capability/asr"
	"agentk/internal/capability/imagegen"
	"agentk/internal/capability/mediaunderstand"
	"agentk/internal/capability/tts"
	"agentk/internal/capability/videogen"
	"agentk/internal/capability/webfetch"
	"agentk/internal/capability/websearch"
	"agentk/internal/chat"
)

// 以下方法使 *Active 成为 capability 执行体的客户端来源。
// 查询键始终是显式 model catalog entry name，不读取 Session Profile，也不提供默认模型。

// catalog 返回当前目录；nil Active 视为空目录。
func (a *Active) catalog() *Catalog {
	if a == nil {
		return nil
	}
	return a.Current()
}

// lookupClient 从单次目录快照按名称读取客户端，避免目录热更期间跨快照查表。
func lookupClient[C any](a *Active, table func(*Catalog) map[string]C, name string) (C, bool) {
	var zero C
	cat := a.catalog()
	if cat == nil {
		return zero, false
	}
	client, ok := table(cat)[name]
	return client, ok
}

func (a *Active) VideoClient(name string) (videogen.Client, bool) {
	return lookupClient(a, func(c *Catalog) map[string]videogen.Client { return c.VideoClients }, name)
}

func (a *Active) KlingT2VClient(name string) (videogen.KlingT2VClient, bool) {
	return lookupClient(a, func(c *Catalog) map[string]videogen.KlingT2VClient {
		return c.KlingT2VClients
	}, name)
}

func (a *Active) KlingI2VClient(name string) (videogen.KlingI2VClient, bool) {
	return lookupClient(a, func(c *Catalog) map[string]videogen.KlingI2VClient {
		return c.KlingI2VClients
	}, name)
}

func (a *Active) KlingOmniClient(name string) (videogen.KlingOmniClient, bool) {
	return lookupClient(a, func(c *Catalog) map[string]videogen.KlingOmniClient {
		return c.KlingOmniClients
	}, name)
}

func (a *Active) SeedanceClient(name string) (videogen.SeedanceClient, bool) {
	return lookupClient(a, func(c *Catalog) map[string]videogen.SeedanceClient {
		return c.SeedanceClients
	}, name)
}

func (a *Active) ImageClient(name string) (imagegen.Client, bool) {
	return lookupClient(a, func(c *Catalog) map[string]imagegen.Client { return c.ImageClients }, name)
}

func (a *Active) UnderstandClient(name string) (mediaunderstand.Client, bool) {
	return lookupClient(a, func(c *Catalog) map[string]mediaunderstand.Client {
		return c.UnderstandClients
	}, name)
}

func (a *Active) SearchClient(name string) (websearch.Client, bool) {
	return lookupClient(a, func(c *Catalog) map[string]websearch.Client { return c.SearchClients }, name)
}

func (a *Active) ProductDetailClient() (webfetch.ProductDetailClient, bool) {
	cat := a.catalog()
	if cat == nil || cat.ProductDetailClient == nil {
		return nil, false
	}
	return cat.ProductDetailClient, true
}

func (a *Active) TTSClient(name string) (tts.Client, bool) {
	return lookupClient(a, func(c *Catalog) map[string]tts.Client { return c.TTSClients }, name)
}

func (a *Active) ASRClient(name string) (asr.Client, bool) {
	return lookupClient(a, func(c *Catalog) map[string]asr.Client { return c.ASRClients }, name)
}

// EditClient 同时返回目录配置的参考图数量上限。
func (a *Active) EditClient(name string) (imagegen.EditClient, int, bool) {
	cat := a.catalog()
	if cat == nil {
		return nil, 0, false
	}
	entry, ok := cat.EditClients[name]
	if !ok {
		return nil, 0, false
	}
	return entry.Client, entry.MaxReferenceImages, true
}

// ChatClient 从完整 ChatModel 条目返回 client。
func (a *Active) ChatClient(name string) (chat.Client, bool) {
	cat := a.catalog()
	if cat == nil {
		return nil, false
	}
	entry, ok := cat.Chat[name]
	if !ok {
		return nil, false
	}
	return entry.Client, true
}
