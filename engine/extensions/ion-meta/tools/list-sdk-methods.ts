// ion_list_sdk_methods tool -- IonContext method reference.
//
// Returns the full IonContext method list with one-line descriptions
// pulled from the bundled SDK's `types.ts`. The parser lives in
// `catalog.ts`; this tool is a thin wrapper that adds optional filtering.

import type { ToolDef } from '../../sdk/ion-sdk'
import { getSDKMethods } from '../catalog'

interface ListSDKMethodsParams {
  /** Exact method name. Returns the single method entry. */
  name?: string
  /** Substring filter applied to method name and signature. */
  contains?: string
}

export const listSDKMethodsTool: ToolDef = {
  name: 'ion_list_sdk_methods',
  description:
    'Return the IonContext method list with one-line descriptions. Use to verify a method exists before referencing it. Optional filtering by exact name or substring.',
  parameters: {
    type: 'object',
    properties: {
      name: {
        type: 'string',
        description: 'Exact method name (e.g. dispatchAgent). Returns the single matching entry.',
      },
      contains: {
        type: 'string',
        description: 'Case-insensitive substring filter; matches the method name and signature.',
      },
    },
  },
  execute: async (params: ListSDKMethodsParams) => {
    const methods = getSDKMethods()
    if (params.name) {
      const hit = methods.find(m => m.name === params.name)
      if (!hit) {
        return {
          content: JSON.stringify(
            { error: `Unknown method: ${params.name}`, known: methods.map(m => m.name) },
            null,
            2,
          ),
          isError: true,
        }
      }
      return { content: JSON.stringify(hit, null, 2) }
    }
    if (params.contains) {
      const needle = params.contains.toLowerCase()
      const filtered = methods.filter(
        m => m.name.toLowerCase().includes(needle) || m.signature.toLowerCase().includes(needle),
      )
      return {
        content: JSON.stringify({ filter: params.contains, methods: filtered }, null, 2),
      }
    }
    return {
      content: JSON.stringify({ total: methods.length, methods }, null, 2),
    }
  },
}
