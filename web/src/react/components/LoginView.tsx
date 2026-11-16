import React, { useState } from 'react'
import { Alert, Button, Card, Input, Space, Typography } from 'antd'
import { KeyOutlined, SafetyCertificateOutlined } from '@ant-design/icons'

interface LoginViewProps {
  reauthNotice: string
  hasPreservedDraftOrKey: boolean
  onLogin: (token: string) => Promise<void>
  themeControl?: React.ReactNode
}

export const LoginView: React.FC<LoginViewProps> = ({
  reauthNotice,
  hasPreservedDraftOrKey,
  onLogin,
  themeControl,
}) => {
  const [tokenInput, setTokenInput] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!tokenInput.trim() || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await onLogin(tokenInput.trim())
      setTokenInput('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '认证失败，请核对访问令牌。')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="ws-login-shell">
      <div className="ws-login-container">
        <div className="ws-login-header">
          <Space align="center" size="middle">
            <div className="ws-brand-mark">
              <SafetyCertificateOutlined />
            </div>
            <div>
              <Typography.Title level={1} style={{ margin: 0 }}>
                wscyber
              </Typography.Title>
              <Typography.Text type="secondary">
                Web/API 安全研究工作台
              </Typography.Text>
            </div>
          </Space>
          {themeControl}
        </div>

        <Card className="ws-login-card">
          <form onSubmit={handleSubmit} className="ws-form-stack">
            {reauthNotice && (
              <Alert
                type="warning"
                showIcon
                title={reauthNotice}
                description={
                  hasPreservedDraftOrKey
                    ? '当前内存草稿与待核对提交已保留，重新登录后可继续操作。'
                    : undefined
                }
              />
            )}

            {error && <Alert type="error" showIcon title={error} />}

            <div className="ws-field-group">
              <label htmlFor="auth-token-input" className="ws-field-label">
                访问令牌
              </label>
              <Input.Password
                id="auth-token-input"
                size="large"
                prefix={<KeyOutlined />}
                placeholder="输入访问令牌"
                value={tokenInput}
                disabled={submitting}
                autoComplete="off"
                onChange={(e) => setTokenInput(e.target.value)}
              />
            </div>

            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={submitting}
              disabled={!tokenInput.trim()}
            >
              进入工作台
            </Button>
          </form>
        </Card>
      </div>
    </div>
  )
}
