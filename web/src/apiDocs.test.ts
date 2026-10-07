import { describe, it, expect, beforeEach, vi } from 'vitest'

const swagger = vi.hoisted(() => vi.fn())
vi.mock('swagger-ui-dist/swagger-ui-es-bundle.js', () => ({ default: swagger }))
beforeEach(() => { vi.resetModules(); swagger.mockReset(); document.body.innerHTML = '<div id="swagger-ui"></div>' })

describe('authenticated API documentation entry', () => {
  it('initializes Swagger against the same-origin schema with session credentials', async () => {
    await import('./apiDocs')
    expect(swagger).toHaveBeenCalledExactlyOnceWith({ url: '/api/openapi.json', dom_id: '#swagger-ui', deepLinking: true, displayOperationId: false, docExpansion: 'none', tryItOutEnabled: false, withCredentials: true })
  })
  it('propagates renderer initialization failure', async () => {
    swagger.mockImplementationOnce(() => { throw new Error('schema renderer unavailable') })
    await expect(import('./apiDocs')).rejects.toThrow('schema renderer unavailable')
  })
})
