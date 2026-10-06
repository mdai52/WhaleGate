/**
 * 各 AI 客户端的接入方式与配置生成。
 *
 * 统一放在一处，避免「接入指南」页与「密钥管理」页各维护一份而逐渐不一致。
 */

/** 可下载的配置片段 */
export interface ClientConfigFile {
  filename: string
  /** 文件内容 */
  text: string
  /** MIME，浏览器下载时用于区分类型 */
  mime: string
}

export interface ClientPreset {
  key: string
  name: string
  desc: string
  /** 客户端要求的网关地址形态：多数客户端要带 /v1，Anthropic 系客户端只要根地址 */
  endpoint: 'with-v1' | 'root'
  /** 填写要点 */
  tips: string[]
  /**
   * 可下载的配置文件。
   *
   * 只在该客户端确实以配置文件方式接入时才提供——GUI 客户端（Cherry Studio、
   * Cursor 等）没有可导入的配置文件，硬造一个 .json 只会让用户拿着无效文件
   * 去导入然后失败，不如不给下载按钮。
   */
  config?: (ctx: ClientContext) => ClientConfigFile
}

/** 生成配置所需的三要素 */
export interface ClientContext {
  /** 网关根地址，如 https://gw.example.com */
  origin: string
  /** 完整密钥明文 */
  apiKey: string
  /** 模型名，可选 */
  model?: string
}

export const CLIENT_PRESETS: ClientPreset[] = [
  {
    key: 'claude-code',
    name: 'Claude Code / CCSwitch',
    desc: 'Anthropic 官方 CLI 及其供应商切换器',
    endpoint: 'root',
    tips: [
      'Claude Code：设置环境变量 ANTHROPIC_BASE_URL 与 ANTHROPIC_AUTH_TOKEN 后直接运行 claude',
      'CCSwitch：新增供应商，类型选 Claude Code，Base URL 与 Token 填下面两个值',
      '也可把 env 写进 Claude Code 的 settings.json',
    ],
    config: (ctx) => {
      const key = ctx.apiKey.trim() || '<在此填入完整密钥>'
      return {
        filename: 'whalegate-claude-code.sh',
        mime: 'text/plain;charset=utf-8',
        text: `#!/usr/bin/env bash
# 鲸闸 WhaleGate · Claude Code 接入
# 用法：source ./whalegate-claude-code.sh

export ANTHROPIC_BASE_URL="${ctx.origin}"
export ANTHROPIC_AUTH_TOKEN="${key}"
`,
      }
    },
  },
  {
    key: 'cherry-studio',
    name: 'Cherry Studio',
    desc: '桌面客户端，支持多提供商与知识库',
    endpoint: 'with-v1',
    tips: [
      '设置 → 模型服务 → 添加提供商，类型选「OpenAI」',
      'API 地址填下方地址，API 密钥填完整明文',
      '在「模型」里点添加，填入要使用的模型名',
    ],
    // Cherry Studio 没有可导入的提供商配置文件，只能按上面要点在界面里填写；
    // 此前下载的是一个 .json 后缀的纯文本，导入会失败，因此不再提供下载。
  },
  {
    key: 'chatbox',
    name: 'ChatBox',
    desc: '桌面与移动端，配置简单',
    endpoint: 'with-v1',
    tips: [
      '设置 → 模型 → 添加自定义提供方',
      'API 模式选「OpenAI API 兼容」',
      'API 域名填下方地址，密钥填完整明文',
    ],
  },
  {
    key: 'nextchat',
    name: 'NextChat',
    desc: '网页版 / 桌面版聊天客户端',
    endpoint: 'with-v1',
    tips: [
      '设置 → 自定义接口',
      '接口地址填下方地址；若客户端会自动补 /v1，则去掉末尾的 /v1',
      '填入完整明文密钥与模型名',
    ],
  },
  {
    key: 'lobechat',
    name: 'LobeChat',
    desc: '网页版聊天客户端与服务端部署',
    endpoint: 'with-v1',
    tips: [
      '设置 → 语言模型 → OpenAI',
      '填入 API Key 与代理地址（下方地址）',
      '模型列表会自动拉取，为空时可手动添加',
    ],
  },
  {
    key: 'open-webui',
    name: 'Open WebUI',
    desc: '自托管网页客户端',
    endpoint: 'with-v1',
    tips: [
      '管理员设置 → 连接 → OpenAI API',
      'API 地址填下方地址，密钥填完整明文',
      '保存后模型下拉会自动出现可用模型',
    ],
  },
  {
    key: 'cursor',
    name: 'Cursor',
    desc: 'AI 代码编辑器',
    endpoint: 'with-v1',
    tips: [
      '设置 → Models → OpenAI API Key',
      '打开「Override OpenAI Base URL」，填入下方地址',
      '点 Verify 校验；自定义模型名填要用的模型',
    ],
  },
  {
    key: 'cline',
    name: 'Cline / Roo Code',
    desc: 'VS Code 编码助手插件',
    endpoint: 'with-v1',
    tips: [
      '设置 → API Provider 选「OpenAI Compatible」',
      'Base URL 填下方地址，API Key 填完整明文',
      'Model ID 填要使用的模型名',
    ],
  },
  {
    key: 'continue',
    name: 'Continue',
    desc: 'VS Code / JetBrains 补全与对话插件',
    endpoint: 'with-v1',
    tips: [
      '编辑 config.json 的 models 数组，新增一条 provider 为 openai 的配置',
      'apiBase 填下方地址，apiKey 填完整明文，model 填模型名',
    ],
    config: (ctx) => {
      const key = ctx.apiKey.trim() || '<在此填入完整密钥>'
      const model = (ctx.model ?? '').trim() || '<模型名>'
      return {
        filename: 'whalegate-continue.json',
        mime: 'application/json;charset=utf-8',
        text: JSON.stringify(
          {
            models: [
              {
                title: `WhaleGate · ${model}`,
                provider: 'openai',
                model,
                apiBase: `${ctx.origin}/v1`,
                apiKey: key,
              },
            ],
          },
          null,
          2,
        ),
      }
    },
  },
]

/** 该客户端应填入的 API 地址。 */
export function endpointFor(preset: ClientPreset, origin: string): string {
  return preset.endpoint === 'root' ? origin : `${origin}/v1`
}

/** 生成该客户端可下载的配置；没有配置文件形态时返回 null。 */
export function configFor(preset: ClientPreset, ctx: ClientContext): ClientConfigFile | null {
  return preset.config ? preset.config(ctx) : null
}

/**
 * 生成该客户端的可复制配置。
 *
 * Anthropic 系客户端用环境变量，其余按 OpenAI 兼容的三要素给出。
 */
export function snippetFor(preset: ClientPreset, ctx: ClientContext): string {
  const key = ctx.apiKey.trim() || '<在此填入完整密钥>'
  const model = (ctx.model ?? '').trim() || '<模型名>'

  if (preset.endpoint === 'root') {
    const settings = JSON.stringify(
      { env: { ANTHROPIC_BASE_URL: ctx.origin, ANTHROPIC_AUTH_TOKEN: key } },
      null,
      2,
    )
    return `export ANTHROPIC_BASE_URL="${ctx.origin}"
export ANTHROPIC_AUTH_TOKEN="${key}"

# 或写入 Claude Code 的 settings.json：
${settings}`
  }
  return `Base URL:  ${ctx.origin}/v1
API Key:   ${key}
Model:     ${model}`
}
