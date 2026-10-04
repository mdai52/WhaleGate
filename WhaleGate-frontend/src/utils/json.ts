/** JSON 文本编辑的解析辅助，用于渠道高级配置与参数调试。 */

export interface ParseResult<T> {
  ok: boolean
  value: T
  error?: string
}

/** 解析 JSON 文本；空文本返回 fallback。 */
export function parseJSON<T>(text: string, fallback: T): ParseResult<T> {
  const trimmed = (text ?? '').trim()
  if (!trimmed) {
    return { ok: true, value: fallback }
  }
  try {
    return { ok: true, value: JSON.parse(trimmed) as T }
  } catch (e) {
    return { ok: false, value: fallback, error: (e as Error).message }
  }
}

/** 把对象序列化为易读的 JSON 文本；空对象返回空串，便于表单留空。 */
export function stringifyJSON(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'object' && Object.keys(value as object).length === 0) {
    return ''
  }
  return JSON.stringify(value, null, 2)
}

/**
 * 把表单里的字符串值推断为数字 / 布尔 / JSON，其余按字符串处理。
 *
 * 键值表单只能承载字符串，但温度、种子这类参数必须是数字，
 * 对象与数组（如 stop、modalities）必须是结构化值，否则上游会拒绝。
 */
export function parseScalarValue(raw: string): unknown {
  const text = (raw ?? '').trim()
  if (text === 'true') {
    return true
  }
  if (text === 'false') {
    return false
  }
  if (text !== '' && !Number.isNaN(Number(text))) {
    return Number(text)
  }
  if (text.startsWith('{') || text.startsWith('[')) {
    const parsed = parseJSON<unknown>(text, null)
    return parsed.ok ? parsed.value : raw
  }
  return raw
}

/** 把键值表单（值均为字符串）整体转换为带类型推断的对象，并忽略空键。 */
export function parseValueRecord(record: Record<string, string>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [key, raw] of Object.entries(record ?? {})) {
    const name = key.trim()
    if (name) {
      out[name] = parseScalarValue(raw)
    }
  }
  return out
}
