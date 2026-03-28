/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState, useRef } from 'react';
import {
  Banner,
  Button,
  Col,
  Row,
  Spin,
  Table,
  Typography,
  Tag,
  Popconfirm,
  Toast,
  Switch,
  InputNumber,
  Input,
  Card,
  Descriptions,
  Modal,
  Form,
} from '@douyinfe/semi-ui';
import {
  API,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import {
  IconRefresh,
  IconPlay,
  IconServer,
  IconBolt,
  IconPlus,
  IconDelete,
  IconDownload,
} from '@douyinfe/semi-icons';

const { Text, Title } = Typography;

export default function SettingsProxy(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [testing, setTesting] = useState(false);
  const [statusLoading, setStatusLoading] = useState(true);
  const [showSubscribeModal, setShowSubscribeModal] = useState(false);
  
  const [config, setConfig] = useState({
    enabled: false,
    clash_api_url: 'http://127.0.0.1:9090',
    clash_proxy_group: '🔰国外流量',
    auto_select: true,
    max_delay: 1000,
    cooldown_secs: 60,
    test_url: 'https://api.airforce/v1/models',
    node_rotation: true,
  });
  
  const [status, setStatus] = useState({
    enabled: false,
    current_node: '',
    total_nodes: 0,
    available_nodes: 0,
    cooldown_nodes: 0,
    nodes: [],
  });

  const [subscribes, setSubscribes] = useState([]);
  const [newSubscribe, setNewSubscribe] = useState({ name: '', url: '' });

  const loadConfig = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/proxy/config');
      if (res.data.success) {
        setConfig(res.data.data);
      }
    } catch (error) {
      showError(t('加载配置失败'));
    } finally {
      setLoading(false);
    }
  };

  const loadStatus = async () => {
    setStatusLoading(true);
    try {
      const res = await API.get('/api/proxy/status');
      if (res.data.success) {
        setStatus(res.data.data);
      }
    } catch (error) {
    } finally {
      setStatusLoading(false);
    }
  };

  const loadSubscribes = async () => {
    try {
      const res = await API.get('/api/proxy/subscribe');
      if (res.data.success) {
        setSubscribes(res.data.data || []);
      }
    } catch (error) {
    }
  };

  useEffect(() => {
    loadConfig();
    loadStatus();
    loadSubscribes();
    const interval = setInterval(loadStatus, 30000);
    return () => clearInterval(interval);
  }, []);

  const handleFieldChange = (fieldName) => (value) => {
    setConfig((prev) => ({ ...prev, [fieldName]: value }));
  };

  const onSubmit = async () => {
    setLoading(true);
    try {
      const res = await API.put('/api/proxy/config', config);
      if (res.data.success) {
        showSuccess(t('保存成功'));
        loadStatus();
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(t('保存失败'));
    } finally {
      setLoading(false);
    }
  };

  const handleRefreshNodes = async () => {
    setRefreshing(true);
    try {
      const res = await API.post('/api/proxy/refresh');
      if (res.data.success) {
        showSuccess(t('节点列表已刷新'));
        loadStatus();
      }
    } catch (error) {
      showError(t('刷新失败'));
    } finally {
      setRefreshing(false);
    }
  };

  const handleSwitchNode = async (nodeName) => {
    try {
      const res = await API.post('/api/proxy/switch', { node_name: nodeName });
      if (res.data.success) {
        showSuccess(t('已切换节点'));
        loadStatus();
      }
    } catch (error) {
      showError(t('切换失败'));
    }
  };

  const handleTestNode = async (nodeName) => {
    setTesting(true);
    try {
      const res = await API.post('/api/proxy/test', { node_name: nodeName });
      if (res.data.success) {
        showSuccess(t('节点 {{name}} 延迟: {{delay}}ms', { 
          name: nodeName, 
          delay: res.data.data?.delay || 'N/A' 
        }));
        loadStatus();
      }
    } catch (error) {
      showError(t('测试失败'));
    } finally {
      setTesting(false);
    }
  };

  const handleTestAllNodes = async () => {
    setTesting(true);
    try {
      const res = await API.post('/api/proxy/test', { test_all: true });
      if (res.data.success) {
        showSuccess(t('测试完成'));
        loadStatus();
      }
    } catch (error) {
      showError(t('测试失败'));
    } finally {
      setTesting(false);
    }
  };

  const handleClearCooldown = async (nodeName, clearAll = false) => {
    try {
      const res = await API.post('/api/proxy/clear-cooldown', { 
        node_name: nodeName, 
        clear_all: clearAll 
      });
      if (res.data.success) {
        showSuccess(t('已清除冷却状态'));
        loadStatus();
      }
    } catch (error) {
      showError(t('清除失败'));
    }
  };

  const handleAddSubscribe = async () => {
    if (!newSubscribe.name || !newSubscribe.url) {
      showError('请填写订阅名称和地址');
      return;
    }
    const newSubs = [...subscribes, { ...newSubscribe, id: Date.now(), enabled: true }];
    const res = await API.put('/api/proxy/subscribe', newSubs);
    if (res.data.success) {
      showSuccess('添加成功');
      setSubscribes(newSubs);
      setNewSubscribe({ name: '', url: '' });
      setShowSubscribeModal(false);
    }
  };

  const handleDeleteSubscribe = async (id) => {
    const newSubs = subscribes.filter(s => s.id !== id);
    const res = await API.put('/api/proxy/subscribe', newSubs);
    if (res.data.success) {
      showSuccess('删除成功');
      setSubscribes(newSubs);
    }
  };

  const handleDownloadSubscribe = async (url) => {
    try {
      const res = await API.post('/api/proxy/subscribe/download', { url });
      if (res.data.success) {
        showSuccess('订阅已更新，正在重启代理...');
        setTimeout(() => {
          loadStatus();
          handleRefreshNodes();
        }, 3000);
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError('下载订阅失败');
    }
  };

  const getDelayTag = (delay) => {
    if (delay <= 0) return <Tag color="grey">未测试</Tag>;
    if (delay < 300) return <Tag color="green">{delay}ms</Tag>;
    if (delay < 800) return <Tag color="orange">{delay}ms</Tag>;
    return <Tag color="red">{delay}ms</Tag>;
  };

  const nodeColumns = [
    {
      title: t('节点名称'),
      dataIndex: 'name',
      key: 'name',
      width: 180,
      render: (text, record) => (
        <Text strong={record.name === status.current_node}>
          {text}
          {record.name === status.current_node && (
            <Tag color="blue" style={{ marginLeft: 8 }}>当前</Tag>
          )}
        </Text>
      ),
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      key: 'type',
      width: 70,
      render: (text) => <Tag>{text?.toUpperCase()}</Tag>,
    },
    {
      title: t('延迟'),
      dataIndex: 'delay',
      key: 'delay',
      width: 90,
      render: (delay) => getDelayTag(delay),
    },
    {
      title: t('状态'),
      dataIndex: 'in_cooldown',
      key: 'in_cooldown',
      width: 100,
      render: (inCooldown, record) => {
        if (!inCooldown) return <Tag color="green">可用</Tag>;
        return <Tag color="red">冷却中</Tag>;
      },
    },
    {
      title: t('操作'),
      key: 'action',
      width: 180,
      render: (_, record) => (
        <div style={{ display: 'flex', gap: 4 }}>
          <Button size="small" onClick={() => handleSwitchNode(record.name)} disabled={record.name === status.current_node}>切换</Button>
          <Button size="small" onClick={() => handleTestNode(record.name)}>测试</Button>
          {record.in_cooldown && (
            <Button size="small" type="warning" onClick={() => handleClearCooldown(record.name)}>解除</Button>
          )}
        </div>
      ),
    },
  ];

  const subscribeColumns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '地址', dataIndex: 'url', key: 'url', render: (url) => <Text ellipsis style={{ maxWidth: 300 }}>{url}</Text> },
    { 
      title: '操作', 
      key: 'action',
      width: 200,
      render: (_, record) => (
        <div style={{ display: 'flex', gap: 4 }}>
          <Button size="small" icon={<IconDownload />} onClick={() => handleDownloadSubscribe(record.url)}>更新</Button>
          <Button size="small" type="danger" icon={<IconDelete />} onClick={() => handleDeleteSubscribe(record.id)}>删除</Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      {/* 订阅管理 */}
      <Card
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <IconDownload />
            <span>订阅管理</span>
          </div>
        }
        headerExtraContent={
          <Button icon={<IconPlus />} onClick={() => setShowSubscribeModal(true)}>添加订阅</Button>
        }
        style={{ marginBottom: 16 }}
      >
        <Table
          columns={subscribeColumns}
          dataSource={subscribes}
          pagination={false}
          rowKey="id"
          size="small"
          empty="暂无订阅"
        />
      </Card>

      {/* 代理配置 */}
      <Spin spinning={loading}>
        <Card
          title={
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <IconServer />
              <span>{t('代理配置')}</span>
            </div>
          }
          style={{ marginBottom: 16 }}
        >
          <div style={{ padding: '0 20px' }}>
            <Row style={{ marginBottom: 16 }}>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>启用代理</span>
                  <Switch checked={config.enabled} onChange={handleFieldChange('enabled')} />
                </div>
              </Col>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>节点轮换</span>
                  <Switch checked={config.node_rotation} onChange={handleFieldChange('node_rotation')} disabled={!config.enabled} />
                </div>
              </Col>
            </Row>
            <Row style={{ marginBottom: 16 }}>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>Clash API 地址</span>
                  <Input 
                    value={config.clash_api_url} 
                    onChange={handleFieldChange('clash_api_url')}
                    placeholder="http://127.0.0.1:9090"
                    disabled={!config.enabled}
                    style={{ flex: 1 }}
                  />
                </div>
              </Col>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>代理组名称</span>
                  <Input 
                    value={config.clash_proxy_group} 
                    onChange={handleFieldChange('clash_proxy_group')}
                    placeholder="🔰国外流量"
                    disabled={!config.enabled}
                    style={{ flex: 1 }}
                  />
                </div>
              </Col>
            </Row>
            <Row style={{ marginBottom: 16 }}>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>智能选择</span>
                  <Switch checked={config.auto_select} onChange={handleFieldChange('auto_select')} disabled={!config.enabled} />
                  <span style={{ marginLeft: 8, color: '#888', fontSize: 12 }}>自动选择延迟最低的节点</span>
                </div>
              </Col>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>最大延迟(ms)</span>
                  <InputNumber 
                    value={config.max_delay} 
                    onChange={handleFieldChange('max_delay')}
                    min={100}
                    max={10000}
                    disabled={!config.enabled || !config.auto_select}
                    style={{ width: 150 }}
                  />
                </div>
              </Col>
            </Row>
            <Row style={{ marginBottom: 16 }}>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>冷却时间(秒)</span>
                  <InputNumber 
                    value={config.cooldown_secs} 
                    onChange={handleFieldChange('cooldown_secs')}
                    min={10}
                    max={3600}
                    disabled={!config.enabled}
                    style={{ width: 150 }}
                  />
                </div>
              </Col>
              <Col span={12}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <span style={{ width: 150, textAlign: 'right', marginRight: 12 }}>测试URL</span>
                  <Input 
                    value={config.test_url} 
                    onChange={handleFieldChange('test_url')}
                    placeholder="https://api.airforce/v1/models"
                    disabled={!config.enabled}
                    style={{ flex: 1 }}
                  />
                </div>
              </Col>
            </Row>
            <Row>
              <Col span={24}>
                <Button type="primary" onClick={onSubmit} loading={loading}>{t('保存配置')}</Button>
              </Col>
            </Row>
          </div>
        </Card>
      </Spin>

      {/* 节点状态 */}
      <Card
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <IconBolt />
            <span>{t('节点状态')}</span>
          </div>
        }
        headerExtraContent={
          <div style={{ display: 'flex', gap: 8 }}>
            <Button icon={<IconRefresh />} onClick={handleRefreshNodes} loading={refreshing} disabled={!config.enabled}>{t('刷新')}</Button>
            <Button icon={<IconPlay />} onClick={handleTestAllNodes} loading={testing} disabled={!config.enabled}>{t('测速')}</Button>
          </div>
        }
      >
        <Spin spinning={statusLoading}>
          <Descriptions>
            <Descriptions.Item label={t('当前节点')}><Text strong>{status.current_node || '-'}</Text></Descriptions.Item>
            <Descriptions.Item label={t('总节点')}>{status.total_nodes}</Descriptions.Item>
            <Descriptions.Item label={t('可用')}><Tag color="green">{status.available_nodes}</Tag></Descriptions.Item>
            <Descriptions.Item label={t('冷却')}><Tag color={status.cooldown_nodes > 0 ? 'red' : 'green'}>{status.cooldown_nodes}</Tag></Descriptions.Item>
          </Descriptions>
          {status.cooldown_nodes > 0 && (
            <div style={{ marginBottom: 16 }}>
              <Popconfirm title="确定清除所有冷却？" onConfirm={() => handleClearCooldown('', true)}>
                <Button type="warning" size="small">清除所有冷却</Button>
              </Popconfirm>
            </div>
          )}
          <Table columns={nodeColumns} dataSource={status.nodes} pagination={{ pageSize: 10 }} rowKey="name" size="small" empty="暂无节点" />
        </Spin>
      </Card>

      {/* 添加订阅弹窗 */}
      <Modal
        title="添加订阅"
        visible={showSubscribeModal}
        onCancel={() => setShowSubscribeModal(false)}
        onOk={handleAddSubscribe}
      >
        <Form>
          <Form.Input field="name" label="订阅名称" placeholder="如：机场A" value={newSubscribe.name} onChange={(v) => setNewSubscribe({ ...newSubscribe, name: v })} />
          <Form.Input field="url" label="订阅地址" placeholder="https://..." value={newSubscribe.url} onChange={(v) => setNewSubscribe({ ...newSubscribe, url: v })} />
        </Form>
      </Modal>
    </div>
  );
}