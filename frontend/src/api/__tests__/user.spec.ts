import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

describe('user api oauth binding urls', () => {
  beforeEach(() => {
    vi.resetModules()
    get.mockReset()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('builds third-party bind urls against the bind start endpoint', async () => {
    const { buildOAuthBindingStartURL } = await import('@/api/user')

    expect(buildOAuthBindingStartURL('linuxdo', { redirectTo: '/settings/profile' })).toBe(
      'https://api.example.com/api/v1/auth/oauth/linuxdo/bind/start?redirect=%2Fsettings%2Fprofile&intent=bind_current_user'
    )
    expect(
      buildOAuthBindingStartURL('wechat', {
        redirectTo: '/settings/profile',
        wechatOAuthSettings: {
          wechat_oauth_open_enabled: true,
          wechat_oauth_mp_enabled: false,
          wechat_oauth_mobile_enabled: false
        }
      })
    ).toBe(
      'https://api.example.com/api/v1/auth/oauth/wechat/bind/start?redirect=%2Fsettings%2Fprofile&intent=bind_current_user&mode=open'
    )
  })
})

describe('user api invitee list', () => {
  beforeEach(() => {
    vi.resetModules()
    get.mockReset()
  })

  it('requests the current invitation expert invitees with pagination', async () => {
    const response = {
      items: [],
      total: 0,
      page: 2,
      page_size: 50,
      pages: 0,
    }
    get.mockResolvedValue({ data: response })

    const { getMyInvitees } = await import('@/api/user')

    await expect(getMyInvitees({ page: 2, page_size: 50 })).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/user/aff/invitees', {
      params: { page: 2, page_size: 50 },
    })
  })
})
