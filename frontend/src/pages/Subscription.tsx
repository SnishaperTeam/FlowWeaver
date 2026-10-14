import React, { useCallback, useEffect, useMemo, useRef, useState, useContext } from 'react';
import {
  Box, Typography, Button, TextField, IconButton, Grid, Chip, Divider, CircularProgress,
  InputAdornment,
} from '@mui/material';
import { alpha } from '@mui/material/styles';
import {
  AddCircle, Delete, RefreshCw, Download, CheckCircle, Search, Share, Lock, Stop, Activity,
} from '../lib/icons';
import {
  GetSubscriptions, AddSubscription, UpdateSubscription, DeleteSubscription,
  ActivateSubscription, SelectSubscriptionNode, TestSubscriptionNodeByID,
} from '../api/bindings';
import ProxyNodeRow, { ProxyNode, ProxyGroup, NodeTestResult } from '../components/ProxyNodeRow';
import { useTranslation } from '../i18n/I18nContext';
import { toast } from '../lib/toast';
import { extractErrorMessage, isTimeoutError, runWithConcurrency } from '../lib/utils';
import { SettingsCtx } from '../App';

interface Subscription {
  id: string;
  name: string;
  url: string;
  builtin: boolean;
  active: boolean;
  updated_at: number;
  upload: number;
  download: number;
  total: number;
  left: number;
  expire_time: number;
  node_count: number;
  rule_count: number;
  groups: ProxyGroup[];
  nodes: ProxyNode[];
  selection: Record<string, string>;
  error: string;
}

// Probe up to 4 nodes at a time: fast enough for large lists without
// saturating the backend or the network stack.
const TEST_CONCURRENCY = 4;

// Results are keyed per subscription AND per node, so switching the viewed
// subscription can never mix one provider's numbers into another's.
const resultKey = (subId: string, nodeName: string) => `${subId}//${nodeName}`;

const formatBytes = (bytes: number): string => {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 2)} ${units[i]}`;
};

const formatDate = (unix: number): string => {
  if (!unix || unix <= 0) return '';
  const d = new Date(unix * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
};

const Subscription: React.FC = () => {
  const { t } = useTranslation();
  const { updateCache } = useContext(SettingsCtx);
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState('');

  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [adding, setAdding] = useState(false);
  const [showAdd, setShowAdd] = useState(false);

  // activeId tracks which subscription's node list is being VIEWED; which
  // rules are actually applied is `s.active`, reported by the backend.
  const [activeId, setActiveId] = useState('');
  const [activeGroup, setActiveGroup] = useState('');
  const [search, setSearch] = useState('');

  const [testResults, setTestResults] = useState<Record<string, NodeTestResult>>({});
  const [testingMap, setTestingMap] = useState<Record<string, boolean>>({});
  const [batch, setBatch] = useState<{ running: boolean; done: number; total: number }>({ running: false, done: 0, total: 0 });
  const batchCancelRef = useRef(false);

  const [switchingMode, setSwitchingMode] = useState(false);
  const switchSeqRef = useRef(0);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  const loadData = useCallback(async () => {
    try {
      const list = (await GetSubscriptions()) as Subscription[];
      if (!mountedRef.current) return;
      setSubs(list || []);
      // Keep the viewed subscription in sync with the actually applied one,
      // so a refresh or restart always shows the real configuration.
      setActiveId((prev) => {
        if (prev && (list || []).some((s) => s.id === prev)) return prev;
        return (list || []).find((s) => s.active)?.id || (list || [])[0]?.id || '';
      });
    } catch (err) {
      if (mountedRef.current) toast.error(extractErrorMessage(err));
    } finally {
      if (mountedRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const activeSub = useMemo(
    () => subs.find((s) => s.id === activeId) || null,
    [subs, activeId]
  );

  // The subscription whose rules are really applied right now — the single
  // source of truth for the mode switch highlight.
  const appliedSub = useMemo(() => subs.find((s) => s.active) || null, [subs]);
  const appliedIsDefault = !appliedSub || appliedSub.builtin;

  useEffect(() => {
    if (!activeSub) {
      setActiveGroup('');
      return;
    }
    if (activeSub.builtin || activeSub.groups.length === 0) {
      setActiveGroup('');
      return;
    }
    const stillValid = activeSub.groups.some((g) => g.name === activeGroup);
    if (!stillValid) {
      setActiveGroup(activeSub.groups[0].name);
    }
  }, [activeSub, activeGroup]);

  const handleAdd = async () => {
    if (!url.trim()) {
      toast.error(t('subscriptions.url_required'));
      return;
    }
    setAdding(true);
    try {
      await AddSubscription(name.trim(), url.trim());
      toast.success(t('subscriptions.add_success'));
      setName('');
      setUrl('');
      setShowAdd(false);
      await loadData();
    } catch (err) {
      toast.error(extractErrorMessage(err));
    } finally {
      if (mountedRef.current) setAdding(false);
    }
  };

  const handleUpdate = async (id: string) => {
    setBusyId(id);
    try {
      await UpdateSubscription(id);
      toast.success(t('subscriptions.update_success'));
      await loadData();
    } catch (err) {
      toast.error(extractErrorMessage(err));
    } finally {
      if (mountedRef.current) setBusyId('');
    }
  };

  const handleDelete = async (sub: Subscription) => {
    if (!confirm(t('subscriptions.delete_confirm', { name: sub.name }))) return;
    setBusyId(sub.id);
    try {
      await DeleteSubscription(sub.id);
      toast.success(t('common.success'));
      if (activeId === sub.id) setActiveId('');
      setTestResults((prev) => {
        const next: Record<string, NodeTestResult> = {};
        for (const [key, value] of Object.entries(prev)) {
          if (!key.startsWith(`${sub.id}//`)) next[key] = value;
        }
        return next;
      });
      await loadData();
    } catch (err) {
      toast.error(extractErrorMessage(err));
    } finally {
      if (mountedRef.current) setBusyId('');
    }
  };

  // Activate a subscription (or the built-in default rules) on the backend.
  // The seq guard makes the LAST user choice win: a slow older request can
  // never overwrite a newer switch. On failure the list is re-fetched so the
  // UI rolls back to whatever the backend really applied.
  const activateSubscription = async (target: Subscription) => {
    if (switchingMode || target.active) return;
    const seq = ++switchSeqRef.current;
    setSwitchingMode(true);
    try {
      await ActivateSubscription(target.id);
      if (seq !== switchSeqRef.current || !mountedRef.current) return;
      toast.success(
        target.builtin
          ? t('subscriptions.switch_default_success')
          : t('subscriptions.switch_sub_success', { name: target.name })
      );
      setActiveId(target.id);
      await loadData();
      if (seq !== switchSeqRef.current || !mountedRef.current) return;
      updateCache({ rulesVersion: Date.now() });
    } catch (err) {
      if (seq !== switchSeqRef.current || !mountedRef.current) return;
      toast.error(t('subscriptions.switch_failed'), extractErrorMessage(err));
      await loadData();
    } finally {
      if (seq === switchSeqRef.current && mountedRef.current) setSwitchingMode(false);
    }
  };

  const handleSelectNode = async (nodeName: string) => {
    if (!activeSub || !activeGroup) return;
    try {
      await SelectSubscriptionNode(activeSub.id, activeGroup, nodeName);
      if (!mountedRef.current) return;
      setSubs((prev) =>
        prev.map((s) =>
          s.id === activeSub.id
            ? { ...s, selection: { ...s.selection, [activeGroup]: nodeName } }
            : s
        )
      );
      toast.success(t('subscriptions.node_selected', { name: nodeName }));
    } catch (err) {
      toast.error(extractErrorMessage(err));
    }
  };

  // Probe one node the Clash Verge way: the test request leaves through the
  // node itself, so the number is real usable latency. Results merge into the
  // per-node map; a failure records a timeout/error status instead of wiping
  // the entry, and other nodes' results are never touched. toasts are
  // suppressed during batch runs.
  const runNodeTest = async (subId: string, node: ProxyNode, silent: boolean) => {
    const key = resultKey(subId, node.name);
    setTestingMap((prev) => ({ ...prev, [key]: true }));
    try {
      const ms = await TestSubscriptionNodeByID(subId, node.name);
      if (!mountedRef.current) return;
      setTestResults((prev) => ({
        ...prev,
        [key]: { latency: ms, status: 'success', timestamp: Date.now() },
      }));
    } catch (err) {
      if (!mountedRef.current) return;
      const msg = extractErrorMessage(err);
      setTestResults((prev) => ({
        ...prev,
        [key]: {
          latency: null,
          status: isTimeoutError(msg) ? 'timeout' : 'error',
          timestamp: Date.now(),
          error: msg,
        },
      }));
      if (!silent) toast.error(t('subscriptions.node_test_failed', { name: node.name }), msg);
    } finally {
      if (mountedRef.current) setTestingMap((prev) => ({ ...prev, [key]: false }));
    }
  };

  const handleTest = (node: ProxyNode) => {
    if (!activeSub) return;
    void runNodeTest(activeSub.id, node, false);
  };

  const cancelBatch = () => { batchCancelRef.current = true; };

  // Test every node in the current group, or all nodes of the subscription.
  // Concurrency is capped and each result lands independently in the map;
  // progress updates as individual nodes finish.
  const handleTestAll = async (scope: 'group' | 'all') => {
    if (!activeSub || batch.running) return;
    const group = activeSub.groups.find((g) => g.name === activeGroup);
    let pool: ProxyNode[];
    if (scope === 'group' && group && !activeSub.builtin) {
      const byName = new Map(activeSub.nodes.map((n) => [n.name, n]));
      pool = group.members
        .map((m) => byName.get(m))
        .filter((n): n is ProxyNode => Boolean(n));
    } else {
      pool = [...activeSub.nodes];
    }
    if (pool.length === 0) return;

    const subId = activeSub.id;
    batchCancelRef.current = false;
    setBatch({ running: true, done: 0, total: pool.length });
    let done = 0;
    await runWithConcurrency(
      pool.map((node) => async () => {
        await runNodeTest(subId, node, true);
        done += 1;
        if (mountedRef.current) setBatch((prev) => ({ ...prev, done }));
      }),
      TEST_CONCURRENCY,
      () => batchCancelRef.current,
    );
    if (mountedRef.current) setBatch({ running: false, done: 0, total: 0 });
  };

  const visibleNodes = useMemo(() => {
    if (!activeSub || activeSub.nodes.length === 0) return [];
    const group = activeSub.groups.find((g) => g.name === activeGroup);
    const members = group ? group.members : [];
    const byName = new Map(activeSub.nodes.map((n) => [n.name, n]));
    const ordered = members
      .map((m) => byName.get(m))
      .filter((n): n is ProxyNode => Boolean(n));
    const rest = activeSub.nodes.filter((n) => !members.includes(n.name));
    const all = group ? [...ordered, ...rest] : activeSub.nodes;

    const q = search.trim().toLowerCase();
    if (!q) return all;
    return all.filter(
      (n) => n.name.toLowerCase().includes(q) || n.server.toLowerCase().includes(q)
    );
  }, [activeSub, activeGroup, search]);

  const availableSubs = useMemo(() => subs.filter((s) => !s.builtin), [subs]);

  const renderResultBadge = (subId: string, node: ProxyNode) => {
    const key = resultKey(subId, node.name);
    return (
      <ProxyNodeRow
        key={node.name}
        node={node}
        selected={activeSub?.selection[activeGroup] === node.name}
        result={testResults[key]}
        testing={Boolean(testingMap[key])}
        onSelect={() => handleSelectNode(node.name)}
        onTest={() => handleTest(node)}
      />
    );
  };

  return (
    <Box sx={{ flexGrow: 1, minHeight: 0, width: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box sx={{ pt: 4, flexShrink: 0, display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'flex-end' }, gap: 2, mb: 4 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Box sx={{ p: 1.25, borderRadius: 1.5, border: 1, color: 'primary.main', bgcolor: (theme) => alpha(theme.palette.primary.main, 0.1), borderColor: (theme) => alpha(theme.palette.primary.main, 0.1), display: 'flex' }}>
            <Share size={20} />
          </Box>
          <Typography variant="h5" sx={{ fontWeight: 700, letterSpacing: '-0.02em' }}>{t('subscriptions.title')}</Typography>
        </Box>
        <Button onClick={() => setShowAdd((v) => !v)} variant="outlined" size="small" startIcon={<AddCircle size={14} />}>
          {t('subscriptions.add')}
        </Button>
      </Box>

      <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', overflowX: 'hidden', pb: 6, display: 'flex', flexDirection: 'column', gap: 4 }}>
        {showAdd && (
          <Box sx={{ p: 2.5, bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: 2, boxShadow: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <Typography variant="caption" sx={{ fontSize: 11, color: 'text.secondary' }}>{t('subscriptions.add_hint')}</Typography>
            <Box sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap' }}>
              <TextField
                size="small"
                label={t('subscriptions.name')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('subscriptions.name_placeholder')}
                sx={{ flex: '1 1 14rem', '& input': { fontSize: '0.8rem' } }}
              />
              <TextField
                data-tut="sub-input"
                size="small"
                label={t('subscriptions.url')}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://"
                sx={{ flex: '2 1 22rem', '& input': { fontSize: '0.8rem' } }}
              />
              <Button onClick={handleAdd} disabled={adding} variant="contained" size="small" startIcon={adding ? <CircularProgress size={12} /> : <Download size={14} />} sx={{ flexShrink: 0 }}>
                {adding ? t('subscriptions.importing') : t('subscriptions.import')}
              </Button>
            </Box>
          </Box>
        )}

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 0.5, color: 'text.secondary' }}>
            <Share size={18} aria-hidden />
            <Typography variant="body2" sx={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              {t('subscriptions.my_subscriptions')}
            </Typography>
          </Box>

          {/* Rule mode switch: highlight follows the backend-applied entry,
              not the card being viewed, so UI and real config can't drift. */}
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1.5, flexWrap: 'wrap', p: 1.5, bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: 2, boxShadow: 1 }}>
            <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, p: 0.5, border: 1, borderColor: 'divider', borderRadius: 2 }} role="tablist" aria-label={t('subscriptions.mode_aria')}>
              <Button
                role="tab"
                aria-selected={appliedIsDefault}
                size="small"
                disabled={switchingMode}
                onClick={() => {
                  const builtin = subs.find((s) => s.builtin);
                  if (builtin) void activateSubscription(builtin);
                }}
                sx={{
                  px: 2, py: 0.5, borderRadius: 1.5, fontSize: 11, fontWeight: 700,
                  color: appliedIsDefault ? 'primary.main' : 'text.secondary',
                  bgcolor: (theme) => (appliedIsDefault ? alpha(theme.palette.primary.main, 0.12) : 'transparent'),
                  '&:hover': { bgcolor: (theme) => (appliedIsDefault ? alpha(theme.palette.primary.main, 0.12) : 'action.hover') },
                }}
              >
                {switchingMode && appliedIsDefault ? <CircularProgress size={12} sx={{ mr: 0.75 }} /> : null}
                {t('subscriptions.mode_default')}
              </Button>
              <Button
                role="tab"
                aria-selected={!appliedIsDefault}
                size="small"
                disabled={switchingMode || availableSubs.length === 0}
                onClick={() => {
                  const target = availableSubs.find((s) => s.active) || availableSubs[0];
                  if (target) void activateSubscription(target);
                }}
                sx={{
                  px: 2, py: 0.5, borderRadius: 1.5, fontSize: 11, fontWeight: 700,
                  color: !appliedIsDefault ? 'primary.main' : 'text.secondary',
                  bgcolor: (theme) => (!appliedIsDefault ? alpha(theme.palette.primary.main, 0.12) : 'transparent'),
                  '&:hover': { bgcolor: (theme) => (!appliedIsDefault ? alpha(theme.palette.primary.main, 0.12) : 'action.hover') },
                }}
              >
                {switchingMode && !appliedIsDefault ? <CircularProgress size={12} sx={{ mr: 0.75 }} /> : null}
                {t('subscriptions.mode_subscription')}
              </Button>
            </Box>
            <Typography variant="caption" sx={{ fontSize: 11, fontWeight: 700, color: 'text.secondary', display: 'flex', alignItems: 'center', gap: 0.75 }}>
              {switchingMode ? (
                <>
                  <Activity size={13} aria-hidden />
                  {t('subscriptions.mode_switching')}
                </>
              ) : appliedIsDefault ? (
                t('subscriptions.rules_source_default')
              ) : (
                t('subscriptions.rules_source_sub', { name: appliedSub?.name || '' })
              )}
            </Typography>
          </Box>

          {loading ? (
            <Box sx={{ py: 8, display: 'flex', justifyContent: 'center' }}>
              <CircularProgress size={22} />
            </Box>
          ) : subs.length === 0 ? (
            <Box sx={{ py: 10, display: 'flex', flexDirection: 'column', alignItems: 'center', color: 'text.secondary', opacity: 0.7, bgcolor: 'background.paper', border: '1px dashed', borderColor: 'divider', borderRadius: 2 }}>
              <Lock size={30} strokeWidth={1.5} />
              <Typography variant="body2" sx={{ mt: 1.5 }}>{t('subscriptions.no_subscriptions')}</Typography>
            </Box>
          ) : (
            <Grid container spacing={2}>
              {subs.map((s) => {
                const sUsed = s.upload + s.download;
                const sPct = s.total > 0 ? Math.min(100, Math.round((sUsed / s.total) * 100)) : 0;
                return (
                  <Grid key={s.id} size={{ xs: 12, md: 6, xl: 4 }}>
                    <Box
                      onClick={() => setActiveId(s.id)}
                      sx={{
                        p: 2.5, bgcolor: 'background.paper', border: 1,
                        borderColor: s.active ? 'primary.main' : 'divider',
                        borderRadius: 2, boxShadow: 1, transition: 'all 0.2s',
                        cursor: 'pointer',
                        '&:hover': { boxShadow: 3, borderColor: 'primary.main' },
                      }}
                    >
                      <Box sx={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 1.5 }}>
                        <Box sx={{ minWidth: 0 }}>
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Typography variant="body2" sx={{ fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {s.name}
                            </Typography>
                            {s.active && <CheckCircle size={14} aria-hidden style={{ color: 'var(--mui-palette-primary-main)', flexShrink: 0 }} />}
                          </Box>
                          <Typography variant="caption" sx={{ fontSize: 10, color: 'text.secondary', fontWeight: 700, display: 'block', mt: 0.25 }}>
                            {s.builtin
                              ? t('subscriptions.builtin_badge')
                              : t('subscriptions.node_count', { count: s.node_count })}
                          </Typography>
                        </Box>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
                          {!s.builtin && (
                            <>
                              <IconButton
                                size="small"
                                aria-label={t('subscriptions.update_aria', { name: s.name })}
                                disabled={busyId === s.id}
                                onClick={(e) => { e.stopPropagation(); handleUpdate(s.id); }}
                                sx={{ color: 'text.secondary', '&:hover': { color: 'primary.main' } }}
                              >
                                <RefreshCw size={15} />
                              </IconButton>
                              <IconButton
                                size="small"
                                aria-label={t('subscriptions.delete_aria', { name: s.name })}
                                disabled={busyId === s.id}
                                onClick={(e) => { e.stopPropagation(); handleDelete(s); }}
                                sx={{ color: 'text.secondary', '&:hover': { color: 'error.main' } }}
                              >
                                <Delete size={15} />
                              </IconButton>
                            </>
                          )}
                        </Box>
                      </Box>

                      {s.error && (
                        <Typography variant="caption" sx={{ display: 'block', mt: 1, fontSize: 10, fontWeight: 700, color: 'error.main' }}>
                          {s.error}
                        </Typography>
                      )}

                      {s.total > 0 && (
                        <Box sx={{ mt: 2 }}>
                          <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.75 }}>
                            <Typography variant="caption" sx={{ fontSize: 10, fontWeight: 700, color: 'text.secondary' }}>
                              {t('subscriptions.remaining')}
                            </Typography>
                            <Typography variant="caption" sx={{ fontSize: 10, fontWeight: 700 }}>
                              {formatBytes(s.left < 0 ? 0 : s.left)} / {formatBytes(s.total)}
                            </Typography>
                          </Box>
                          <Box sx={{ height: 5, borderRadius: 3, bgcolor: 'action.hover', overflow: 'hidden' }}>
                            <Box sx={{
                              height: '100%', borderRadius: 3,
                              width: `${sPct}%`,
                              bgcolor: sPct > 90 ? 'error.main' : sPct > 70 ? 'warning.main' : 'primary.main',
                              transition: 'width 0.3s',
                            }} />
                          </Box>
                          <Typography variant="caption" sx={{ display: 'block', mt: 0.75, fontSize: 10, color: 'text.secondary', fontWeight: 700 }}>
                            {t('subscriptions.used', { used: formatBytes(sUsed) })}
                          </Typography>
                        </Box>
                      )}

                      {s.expire_time > 0 && (
                        <Box sx={{ mt: 1.5 }}>
                          <Chip
                            size="small"
                            label={t('subscriptions.expires', { date: formatDate(s.expire_time) })}
                            sx={{ fontSize: 10, fontWeight: 700, height: 20 }}
                          />
                        </Box>
                      )}

                      {!s.active && (
                        <Button
                          fullWidth
                          size="small"
                          variant="outlined"
                          disabled={switchingMode || busyId === s.id || (!s.builtin && s.node_count === 0)}
                          onClick={(e) => { e.stopPropagation(); void activateSubscription(s); }}
                          sx={{ mt: 2, fontSize: 11, fontWeight: 700 }}
                        >
                          {switchingMode
                            ? t('subscriptions.mode_switching')
                            : s.builtin
                              ? t('subscriptions.activate_default')
                              : t('subscriptions.activate')}
                        </Button>
                      )}
                    </Box>
                  </Grid>
                );
              })}
            </Grid>
          )}
        </Box>

        {activeSub && !activeSub.builtin && (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <Divider />
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', px: 0.5, flexWrap: 'wrap', gap: 1 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, color: 'text.secondary' }}>
                <Share size={18} aria-hidden />
                <Typography variant="body2" sx={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  {t('subscriptions.node_selection')}
                </Typography>
              </Box>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                {batch.running && (
                  <>
                    <Typography variant="caption" sx={{ fontSize: 11, fontWeight: 700, color: 'primary.main', display: 'flex', alignItems: 'center', gap: 0.5 }}>
                      <CircularProgress size={11} thickness={5} />
                      {t('subscriptions.test_progress', { done: batch.done, total: batch.total })}
                    </Typography>
                    <Button
                      size="small"
                      variant="outlined"
                      color="error"
                      startIcon={<Stop size={13} />}
                      onClick={cancelBatch}
                      sx={{ fontSize: 11, fontWeight: 700, minWidth: 0 }}
                    >
                      {t('subscriptions.stop_test')}
                    </Button>
                  </>
                )}
                <Button
                  size="small"
                  variant="outlined"
                  disabled={batch.running || visibleNodes.length === 0}
                  onClick={() => void handleTestAll('group')}
                  sx={{ fontSize: 11, fontWeight: 700 }}
                >
                  {t('subscriptions.test_group')}
                </Button>
                <Button
                  size="small"
                  variant="contained"
                  disabled={batch.running || activeSub.nodes.length === 0}
                  onClick={() => void handleTestAll('all')}
                  sx={{ fontSize: 11, fontWeight: 700 }}
                >
                  {t('subscriptions.test_all')}
                </Button>
                <TextField
                  size="small"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder={t('subscriptions.search_placeholder')}
                  slotProps={{ input: { startAdornment: <InputAdornment position="start"><Search size={14} /></InputAdornment> } }}
                  sx={{ width: 220, '& input': { fontSize: '0.78rem' } }}
                />
              </Box>
            </Box>

            {activeSub.groups.length > 1 && (
              <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                {activeSub.groups.map((g) => (
                  <Button
                    key={g.name}
                    size="small"
                    variant={g.name === activeGroup ? 'contained' : 'outlined'}
                    onClick={() => setActiveGroup(g.name)}
                    sx={{ fontSize: 11, fontWeight: 700 }}
                  >
                    {g.name}
                  </Button>
                ))}
              </Box>
            )}

            {activeSub.groups.length > 0 && (
              <Box sx={{ px: 0.5 }}>
                <Typography variant="caption" sx={{ fontSize: 11, fontWeight: 700, color: 'text.secondary' }}>
                  {t('subscriptions.current_selection', { name: activeSub.selection[activeGroup] || t('subscriptions.not_selected') })}
                </Typography>
              </Box>
            )}

            {visibleNodes.length === 0 ? (
              <Box sx={{ py: 8, display: 'flex', flexDirection: 'column', alignItems: 'center', color: 'text.secondary', opacity: 0.7, bgcolor: 'background.paper', border: '1px dashed', borderColor: 'divider', borderRadius: 2 }}>
                <Share size={28} strokeWidth={1.5} />
                <Typography variant="body2" sx={{ mt: 1.5 }}>{t('subscriptions.no_nodes')}</Typography>
              </Box>
            ) : (
              <Box>
                {visibleNodes.map((n) => renderResultBadge(activeSub.id, n))}
              </Box>
            )}
          </Box>
        )}

        {activeSub && activeSub.builtin && appliedIsDefault && (
          <Box sx={{ p: 2.5, display: 'flex', alignItems: 'center', gap: 1.5, bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: 2 }}>
            <CheckCircle size={18} aria-hidden style={{ color: 'var(--mui-palette-success-main)', flexShrink: 0 }} />
            <Box>
              <Typography variant="body2" sx={{ fontWeight: 700 }}>{t('subscriptions.default_rules_active')}</Typography>
              <Typography variant="caption" sx={{ fontSize: 11, color: 'text.secondary' }}>
                {t('subscriptions.default_rules_desc')}
              </Typography>
            </Box>
          </Box>
        )}
      </Box>

    </Box>
  );
};

export default Subscription;
