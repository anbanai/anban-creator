import { useEffect, useId, useMemo } from 'react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { registeredAgentPackForm } from '@/lib/agent-pack-renderers'
import type { AgentPack, AgentPackJSONSchema } from '@/types'

type ExtensionSurface = 'task' | 'plan'

interface AgentPackSchemaFieldsProps {
  pack?: AgentPack
  surface: ExtensionSurface
  value?: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
}

function schemaForSurface(pack: AgentPack, _surface: ExtensionSurface) {
  return pack.schemas?.task_input
}

export function AgentPackSchemaFields({ pack, surface, value = {}, onChange }: AgentPackSchemaFieldsProps) {
  const idPrefix = useId()
  const usesGenericSchema = pack?.surfaces.includes(surface) && !pack.ui?.renderer?.startsWith('custom:')
  const schema = pack && usesGenericSchema ? schemaForSurface(pack, surface) : undefined
  const initialPatch = useMemo(() => schemaInitialPatch(schema, value), [schema, value])

  useEffect(() => {
    if (initialPatch) onChange({ ...value, ...initialPatch })
  }, [initialPatch, onChange, value])

  if (!pack || !pack.surfaces.includes(surface)) return null

  const renderer = pack.ui?.renderer
  if (renderer?.startsWith('custom:')) {
    if (registeredAgentPackForm(renderer)) return null
    return (
      <Alert variant="destructive">
        <AlertDescription>场景表单 {renderer} 尚未注册，暂不能提交。</AlertDescription>
      </Alert>
    )
  }

  if (!schema) return null
  if (schema.type !== 'object' || !schema.properties) {
    return (
      <Alert variant="destructive">
        <AlertDescription>Agent Pack 的扩展 Schema 必须是对象。</AlertDescription>
      </Alert>
    )
  }

  return (
    <FieldGroup data-slot="agent-pack-schema-fields">
      {Object.entries(schema.properties).map(([key, property]) => (
        <SchemaField
          key={key}
          id={`${idPrefix}-${key}`}
          name={key}
          schema={property}
          required={schema.required?.includes(key) ?? false}
          value={value[key]}
          onChange={(next) => onChange({ ...value, [key]: next })}
        />
      ))}
    </FieldGroup>
  )
}

function schemaInitialPatch(schema: AgentPackJSONSchema | undefined, value: Record<string, unknown>) {
  if (schema?.type !== 'object' || !schema.properties) return null
  const patch: Record<string, unknown> = {}
  for (const [key, property] of Object.entries(schema.properties)) {
    if (Object.prototype.hasOwnProperty.call(value, key)) continue
    if (property.default !== undefined) {
      patch[key] = cloneSchemaValue(property.default)
    } else if (property.type === 'boolean' && schema.required?.includes(key)) {
      patch[key] = false
    }
  }
  return Object.keys(patch).length > 0 ? patch : null
}

function cloneSchemaValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(cloneSchemaValue)
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, cloneSchemaValue(item)]))
  }
  return value
}

interface SchemaFieldProps {
  id: string
  name: string
  schema: AgentPackJSONSchema
  required: boolean
  value: unknown
  onChange: (value: unknown) => void
}

function SchemaField({ id, name, schema, required, value, onChange }: SchemaFieldProps) {
  const label = schema.title || name
  const description = schema.description
  const hint = schemaHint(schema, description)
  const placeholder = !description && required ? '必填' : undefined

  if (schema.enum?.length && !hasSafeEnumOptions(schema)) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{label}的枚举配置无效，暂不能提交。</AlertDescription>
      </Alert>
    )
  }

  if (schema.type === 'boolean') {
    return (
      <Field orientation="horizontal">
        <FieldLabel htmlFor={id}>{label}{required ? ' *' : ''}</FieldLabel>
        {hint ? <FieldDescription>{hint}</FieldDescription> : null}
        <Switch id={id} aria-label={label} checked={value === true} onCheckedChange={onChange} />
      </Field>
    )
  }

  if (schema.enum?.length) {
    return (
      <Field>
        <FieldLabel htmlFor={id}>{label}{required ? ' *' : ''}</FieldLabel>
        <Select value={value == null ? null : String(value)} onValueChange={(next) => onChange(enumValue(schema, next))}>
          <SelectTrigger id={id} className="w-full" aria-label={label}>
            <SelectValue placeholder={`请选择${label}`} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {schema.enum.map((option) => <SelectItem key={String(option)} value={String(option)}>{String(option)}</SelectItem>)}
            </SelectGroup>
          </SelectContent>
        </Select>
        {hint ? <FieldDescription>{hint}</FieldDescription> : null}
      </Field>
    )
  }

  if (schema.type === 'number' || schema.type === 'integer') {
    return (
      <Field>
        <FieldLabel htmlFor={id}>{label}{required ? ' *' : ''}</FieldLabel>
        <Input
          id={id}
          aria-label={label}
          type="number"
          min={schema.minimum}
          max={schema.maximum}
          step={schema.type === 'integer' ? 1 : 'any'}
          placeholder={placeholder}
          value={typeof value === 'number' ? value : ''}
          onChange={(event) => onChange(event.target.value === '' ? undefined : Number(event.target.value))}
        />
        {hint ? <FieldDescription>{hint}</FieldDescription> : null}
      </Field>
    )
  }

  if (schema.type === 'string') {
    const common = {
      id,
      'aria-label': label,
      value: typeof value === 'string' ? value : '',
      placeholder,
      onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(event.target.value),
    }
    return (
      <Field>
        <FieldLabel htmlFor={id}>{label}{required ? ' *' : ''}</FieldLabel>
        {schema.format === 'textarea' ? <Textarea {...common} /> : <Input {...common} />}
        {hint ? <FieldDescription>{hint}</FieldDescription> : null}
      </Field>
    )
  }

  return (
    <Alert variant="destructive">
      <AlertDescription>{label} 使用了 Studio 尚未支持的 Schema 类型。</AlertDescription>
    </Alert>
  )
}

/**
 * Pack 未写 description 时，用 Schema 自身的数值约束生成最小可用的补充说明。
 * 必填性已经由标题上的 * 和必填 placeholder 表达，不再重复成一句话。
 */
function schemaHint(
  schema: AgentPackJSONSchema,
  description: string | undefined,
): string | undefined {
  const range: string[] = []
  if (typeof schema.minimum === 'number') range.push(`最小 ${schema.minimum}`)
  if (typeof schema.maximum === 'number') range.push(`最大 ${schema.maximum}`)
  const parts: string[] = []
  if (description) parts.push(description)
  if (range.length > 0) parts.push(`${range.join('，')}。`)
  return parts.length > 0 ? parts.join(' ') : undefined
}

function hasSafeEnumOptions(schema: AgentPackJSONSchema) {
  const identities = new Set<string>()
  return schema.enum?.every((candidate) => {
    const typeMatches = schema.type === 'string'
      ? typeof candidate === 'string'
      : schema.type === 'boolean'
        ? typeof candidate === 'boolean'
        : schema.type === 'integer'
          ? typeof candidate === 'number' && Number.isInteger(candidate)
          : schema.type === 'number'
            ? typeof candidate === 'number' && Number.isFinite(candidate)
            : false
    const identity = String(candidate)
    if (!typeMatches || identities.has(identity)) return false
    identities.add(identity)
    return true
  }) ?? true
}

function enumValue(schema: AgentPackJSONSchema, selected: string | null) {
  return schema.enum?.find((candidate) => String(candidate) === selected)
}
