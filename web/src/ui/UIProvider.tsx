import { useLayoutEffect, type ReactNode } from 'react'
import { App as AntApp, ConfigProvider, theme as antTheme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { springCurve, surfaceTokens, type SurfaceTheme } from './surface.ts'
import './styles.css'

/** 每个独立入口只挂载一个Provider；主题覆盖document及Ant portal，不存储用户数据。 */
export function UIProvider({ appearance, children }: { appearance: SurfaceTheme; children: ReactNode }) {
  const palette = surfaceTokens[appearance]
  useLayoutEffect(() => {
    document.documentElement.dataset.theme = appearance
    for (const [key, value] of Object.entries(palette)) document.documentElement.style.setProperty(`--${key}`, String(value))
    document.documentElement.style.setProperty('--spring-drag', springCurve('drag'))
    document.documentElement.style.setProperty('--spring-check', springCurve('check'))
  }, [appearance, palette])
  return <ConfigProvider locale={zhCN} theme={{ algorithm: appearance === 'dark' ? antTheme.darkAlgorithm : antTheme.defaultAlgorithm, token: {
    colorPrimary: palette.accent, colorInfo: palette.accent, colorText: palette.ink, colorTextSecondary: palette.muted,
    colorBgBase: palette.base, colorBgContainer: palette.plane, colorBgElevated: palette.raised, colorBgLayout: palette.base,
    colorBorder: palette.line, colorBorderSecondary: palette.line, colorFillSecondary: palette.inset,
    colorTextPlaceholder: palette.muted, colorTextDisabled: palette.muted, colorBgContainerDisabled: palette.inset,
    colorTextLightSolid: appearance === 'dark' ? palette.base : palette.plane,
    borderRadius: 6, lineWidth: 1, fontSize: 14, fontSizeSM: 14, fontSizeLG: 16, fontSizeHeading1: 22, fontSizeHeading2: 20, fontSizeHeading3: 16,
    controlHeight: 34, controlHeightSM: 30, lineHeight: 1.5, fontWeightStrong: palette.strong, boxShadow: 'none', boxShadowSecondary: 'none',
    fontFamily: "'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif",
  }, components: {
    Button: { contentFontSizeSM: 14, primaryShadow: 'none', defaultShadow: 'none' }, Tag: { fontSizeSM: 14 },
    Tooltip: { fontSize: 14, colorBgSpotlight: palette.ink, colorTextLightSolid: palette.base }, Tabs: { titleFontSize: 14, horizontalItemPadding: '8px 0', horizontalMargin: '0 0 12px 0' },
    Tree: { titleHeight: 36, indentSize: 16 }, Select: { optionFontSize: 14 }, Segmented: { itemSelectedBg: palette.selected },
    Badge: { textFontSize: 14, textFontSizeSM: 14 }, Table: { cellFontSizeSM: 14, cellPaddingBlock: 10, cellPaddingInline: 12 },
    Card: { bodyPadding: 16, headerPadding: 16, headerHeight: 44, headerFontSize: 16, bodyPaddingSM: 12, headerPaddingSM: 12 },
    Form: { itemMarginBottom: 12, verticalLabelPadding: '0 0 6px' }, Descriptions: { itemPaddingBottom: 10 },
    Drawer: { padding: 16, paddingLG: 16 },
  } }}>
    <AntApp message={{ maxCount: 1, transitionName: '' }}>{children}</AntApp>
  </ConfigProvider>
}
