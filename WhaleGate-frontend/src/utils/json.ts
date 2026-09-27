/** JSON 文本编辑的解析辅助，用于渠道高级配置（别名 / 注入 / 参数能力）。 */

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
