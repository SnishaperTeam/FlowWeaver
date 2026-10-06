import React, { useState } from 'react';
import { Box, ListItem, ListItemButton, alpha } from '@mui/material';
import { CheckCircle, Activity } from '../lib/icons';
import { useTranslation } from '../i18n/I18nContext';

export interface ProxyNode {
  name: string;
  type: string;
  server: string;
  port: number;
  server_name?: string;
  udp: boolean;
  supported?: boolean;
  udp_supported?: boolean;
}

export interface ProxyGroup {
  name: string;
  type: string;
  members: string[];
  selected?: string;
}

interface Props {
  node: ProxyNode;
  selected: boolean;
  delay?: number;
  testing: boolean;
  onSelect: () => void;
  onTest: () => void;
}

const TypeBox: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <Box
    component="span"
    sx={{
      display: 'inline-block',
      border: '1px solid',
      borderColor: (theme) => alpha(theme.palette.text.secondary, 0.36),
      color: (theme) => alpha(theme.palette.text.secondary, 0.72),
      borderRadius: '4px',
      fontSize: 10,
      mx: 0.25,
      px: 0.25,
      lineHeight: 1.6,
      textTransform: 'uppercase',
      userSelect: 'none',
    }}
  >
    {children}
  </Box>
);

const delayColor = (delay: number): 'success.main' | 'warning.main' | 'error.main' => {
  if (delay < 0) return 'error.main';
  if (delay < 300) return 'success.main';
  if (delay < 1000) return 'warning.main';
  return 'error.main';
};

// ProxyNodeRow mirrors the Clash Verge proxy item: fixed 40px rows, a 3px
// left accent for the active node, type capsules and a delay/check affordance
// that swaps on hover.
const ProxyNodeRow: React.FC<Props> = ({ node, selected, delay, testing, onSelect, onTest }) => {
  const { t } = useTranslation();
  const hasDelay = typeof delay === 'number' && delay > 0;
  // Nodes whose protocol cannot be dialled stay visible but inert, so the user
  // can see what the provider offers without being able to pick a dead node.
  const usable = node.supported !== false;

  return (
    <ListItem disablePadding sx={{ mb: 1 }}>
      <ListItemButton
        dense
        disabled={!usable}
        selected={usable && selected}
        onClick={usable ? onSelect : undefined}
        sx={{
          borderRadius: 1,
          bgcolor: 'background.paper',
          border: 1,
          borderColor: 'divider',
          mb: 0.5,
          height: 40,
          opacity: usable ? 1 : 0.5,
          '&:hover': { bgcolor: 'action.hover' },
          '&.Mui-selected': {
            width: 'calc(100% + 3px)',
            ml: '-3px',
            borderLeft: 3,
            borderLeftColor: (theme) =>
              theme.palette.mode === 'light' ? theme.palette.primary.main : theme.palette.primary.light,
            bgcolor: (theme) =>
              alpha(theme.palette.primary.main, theme.palette.mode === 'light' ? 0.15 : 0.35),
            '&:hover': {
              bgcolor: (theme) =>
                alpha(theme.palette.primary.main, theme.palette.mode === 'light' ? 0.15 : 0.35),
            },
          },
        }}
      >
        <Box sx={{ minWidth: 0, flexGrow: 1, display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 0.25 }}>
          <Box
            component="span"
            sx={{
              fontSize: 14,
              color: 'text.primary',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              maxWidth: '100%',
            }}
          >
            {node.name}
          </Box>
          <TypeBox>{node.type}</TypeBox>
          {/* Only advertise UDP when the relay is actually implemented for it,
              not merely because the subscription claims udp: true. */}
          {node.udp_supported && <TypeBox>UDP</TypeBox>}
          {!usable && <TypeBox>{t('subscriptions.unsupported')}</TypeBox>}
        </Box>

        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', flexShrink: 0, ml: 1 }}>
          {!usable ? null : (
          <>
          <Box
            component="span"
            onClick={(e: React.MouseEvent) => {
              e.preventDefault();
              e.stopPropagation();
              onTest();
            }}
            sx={{
              display: hasDelay ? 'none' : 'block',
              px: 0.75,
              py: 0.25,
              borderRadius: 0.5,
              fontSize: 12,
              fontWeight: 700,
              color: 'primary.main',
              cursor: 'pointer',
              '&:hover': { bgcolor: (theme) => alpha(theme.palette.primary.main, 0.15) },
              '.Mui-selected &': { display: 'none' },
              '.MuiListItemButton-root:hover > &': { display: 'block' },
            }}
          >
            {testing ? t('subscriptions.testing') : t('subscriptions.check')}
          </Box>

          {hasDelay && (
            <Box
              component="span"
              onClick={(e: React.MouseEvent) => {
                e.preventDefault();
                e.stopPropagation();
                onTest();
              }}
              sx={{
                px: 0.75,
                py: 0.25,
                borderRadius: 0.5,
                fontSize: 12,
                fontWeight: 700,
                color: delayColor(delay as number),
                cursor: 'pointer',
                '&:hover': { bgcolor: (theme) => alpha(theme.palette.primary.main, 0.15) },
                '.MuiListItemButton-root:hover > &': { display: 'none' },
              }}
            >
              {delay} ms
            </Box>
          )}

          {selected && !hasDelay && !testing && (
            <CheckCircle size={16} aria-hidden style={{ color: 'var(--mui-palette-primary-main)' }} />
          )}

          {testing && <Activity size={14} aria-hidden style={{ marginLeft: 4 }} />}
          </>
          )}
        </Box>
      </ListItemButton>
    </ListItem>
  );
};

export default ProxyNodeRow;
