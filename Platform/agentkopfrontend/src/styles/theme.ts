import type { ThemeConfig } from 'antd';
import { colors, rounded, typography, spacing, shadows } from './design-tokens';

/**
 * Ant Design 全局主题配置
 *
 * 所有颜色、圆角、字号都引用 design-tokens.ts，与 DESIGN.md 保持一致。
 * 修改时先改 DESIGN.md 和 design-tokens.ts，这里自动同步。
 *
 * @see DESIGN.md
 */
export const themeConfig: ThemeConfig = {
  token: {
    // 品牌色
    colorPrimary: colors.primary,
    colorInfo: colors.info,
    colorSuccess: colors.success,
    colorWarning: colors.warning,
    colorError: colors.error,

    // 文字色
    colorText: colors.ink,
    colorTextSecondary: colors.inkSecondary,
    colorTextTertiary: colors.inkTertiary,
    colorTextDisabled: colors.inkDisabled,

    // 背景色
    colorBgContainer: colors.canvas,
    colorBgLayout: colors.canvasSoft,
    colorBgElevated: colors.surface1,

    // 边框
    colorBorder: colors.hairline,
    colorBorderSecondary: colors.hairlineStrong,
    colorSplit: colors.divider,

    // 圆角
    borderRadius: rounded.md,
    borderRadiusLG: rounded.lg,
    borderRadiusSM: rounded.sm,
    borderRadiusXS: rounded.xs,

    // 字号
    fontSize: typography.body.fontSize,
    fontSizeSM: typography.bodySm.fontSize,
    fontSizeLG: typography.bodyLg.fontSize,
    fontSizeHeading1: typography.displayMd.fontSize,
    fontSizeHeading2: typography.heading1.fontSize,
    fontSizeHeading3: typography.heading2.fontSize,
    fontSizeHeading4: typography.heading3.fontSize,
    fontSizeHeading5: typography.bodyLg.fontSize,

    // 行高
    lineHeight: typography.body.lineHeight,
    lineHeightLG: typography.bodyLg.lineHeight,
    lineHeightSM: typography.bodySm.lineHeight,
    lineHeightHeading1: typography.displayMd.lineHeight,
    lineHeightHeading2: typography.heading1.lineHeight,
    lineHeightHeading3: typography.heading2.lineHeight,

    // 阴影
    boxShadow: shadows.sm,
    boxShadowSecondary: shadows.md,

    // 间距
    controlHeight: 32,
    controlHeightSM: 24,
    controlHeightLG: 40,
    controlPaddingHorizontal: spacing.sm,
    controlPaddingHorizontalSM: spacing.xs,
  },

  components: {
    Layout: {
      siderBg: colors.canvas,
      headerBg: colors.canvas,
      bodyBg: colors.canvasSoft,
      headerHeight: 48,
    },
    Menu: {
      itemBorderRadius: rounded.md,
      itemMarginInline: spacing.xs,
      itemMarginBlock: spacing.xxs,
      itemHeight: 40,
      iconSize: 18,
      collapsedWidth: 72,
      collapsedIconSize: 18,
      // 二级菜单默认是浅灰底，与侧边栏 canvas 白色统一
      itemBg: colors.canvas,
      subMenuItemBg: colors.canvas,
      itemSelectedBg: colors.primarySubdued,
      itemSelectedColor: colors.primary,
      itemHoverBg: colors.surface3,
    },
    Button: {
      borderRadius: rounded.md,
      borderRadiusLG: rounded.lg,
      borderRadiusSM: rounded.sm,
      controlHeight: 32,
      controlHeightSM: 24,
      controlHeightLG: 40,
      paddingInline: spacing.md,
      paddingInlineSM: spacing.xs,
      paddingInlineLG: spacing.lg,
      fontWeight: typography.button.fontWeight,
    },
    Card: {
      borderRadiusLG: rounded.lg,
      paddingLG: spacing.lg,
      boxShadowTertiary: shadows.sm,
    },
    Input: {
      borderRadius: rounded.md,
      controlHeight: 32,
      paddingInline: spacing.sm,
    },
    Select: {
      borderRadius: rounded.md,
      controlHeight: 32,
    },
    Table: {
      borderRadius: rounded.md,
      headerBg: colors.canvasSoft,
      headerColor: colors.inkSecondary,
      rowHoverBg: colors.surface3,
      cellPaddingBlock: spacing.sm,
    },
    Modal: {
      borderRadiusLG: rounded.lg,
    },
    Tabs: {
      itemColor: colors.inkSecondary,
      itemSelectedColor: colors.primary,
      itemHoverColor: colors.ink,
      inkBarColor: colors.primary,
    },
    Tag: {
      borderRadiusSM: rounded.xs,
    },
  },
};
