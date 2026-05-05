import React, { useEffect, useState } from 'react';
import { Card, Form, Input, Button, message, Descriptions, Tag, Avatar } from 'antd';
import { UserOutlined, LockOutlined, MailOutlined } from '@ant-design/icons';
import { authApi } from '../api';
import dayjs from 'dayjs';

const Profile: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [profile, setProfile] = useState<any>(null);
  const [passwordForm] = Form.useForm();
  const roleLabels: Record<string, { color: string; text: string }> = {
    owner: { color: 'gold', text: '所有者' },
    admin: { color: 'red', text: '管理员' },
    developer: { color: 'blue', text: '开发者' },
    viewer: { color: 'default', text: '只读' },
  };

  useEffect(() => {
    fetchProfile();
  }, []);

  const fetchProfile = async () => {
    setLoading(true);
    try {
      const res: any = await authApi.getProfile();
      setProfile(res?.user || res);
    } catch (error) {
      console.error(error);
    } finally {
      setLoading(false);
    }
  };

  const handleChangePassword = async (values: any) => {
    try {
      await authApi.changePassword({
        old_password: values.oldPassword,
        new_password: values.newPassword,
      });
      message.success('密码修改成功');
      passwordForm.resetFields();
    } catch (error) {
      console.error(error);
    }
  };

  return (
    <div>
      <h2 style={{ marginBottom: 24 }}>个人中心</h2>

      <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
        {/* 基本信息 */}
        <Card title="基本信息" style={{ flex: 1, minWidth: 400 }} loading={loading}>
          {profile && (
            <>
              <div style={{ textAlign: 'center', marginBottom: 24 }}>
                <Avatar size={80} icon={<UserOutlined />} src={profile.avatar} />
                <h3 style={{ marginTop: 12, marginBottom: 4 }}>{profile.name}</h3>
                <Tag color={roleLabels[profile.role]?.color || 'default'}>
                  {roleLabels[profile.role]?.text || profile.role}
                </Tag>
              </div>
              <Descriptions column={1} bordered size="small">
                <Descriptions.Item label={<><MailOutlined /> 邮箱</>}>
                  {profile.email}
                  {profile.email_verified ? (
                    <Tag color="green" style={{ marginLeft: 8 }}>已验证</Tag>
                  ) : (
                    <Tag color="orange" style={{ marginLeft: 8 }}>未验证</Tag>
                  )}
                </Descriptions.Item>
                <Descriptions.Item label="手机号">{profile.phone || '-'}</Descriptions.Item>
                <Descriptions.Item label="注册时间">
                  {dayjs(profile.created_at).format('YYYY-MM-DD HH:mm')}
                </Descriptions.Item>
                <Descriptions.Item label="最后登录">
                  {profile.last_login_at ? dayjs(profile.last_login_at).format('YYYY-MM-DD HH:mm') : '-'}
                </Descriptions.Item>
              </Descriptions>
            </>
          )}
        </Card>

        {/* 修改密码 */}
        <Card title={<><LockOutlined /> 修改密码</>} style={{ flex: 1, minWidth: 400 }}>
          <Form
            form={passwordForm}
            layout="vertical"
            onFinish={handleChangePassword}
          >
            <Form.Item
              name="oldPassword"
              label="当前密码"
              rules={[{ required: true, message: '请输入当前密码' }]}
            >
              <Input.Password placeholder="请输入当前密码" />
            </Form.Item>
            <Form.Item
              name="newPassword"
              label="新密码"
              rules={[
                { required: true, message: '请输入新密码' },
                { min: 8, message: '密码至少8位' },
                { pattern: /\d/, message: '密码必须包含数字' },
                { pattern: /[^A-Za-z0-9]/, message: '密码必须包含特殊字符' },
              ]}
            >
              <Input.Password placeholder="请输入新密码" />
            </Form.Item>
            <Form.Item
              name="confirmPassword"
              label="确认新密码"
              dependencies={['newPassword']}
              rules={[
                { required: true, message: '请确认新密码' },
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!value || getFieldValue('newPassword') === value) {
                      return Promise.resolve();
                    }
                    return Promise.reject(new Error('两次输入的密码不一致'));
                  },
                }),
              ]}
            >
              <Input.Password placeholder="请再次输入新密码" />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit">
                修改密码
              </Button>
            </Form.Item>
          </Form>
        </Card>
      </div>
    </div>
  );
};

export default Profile;
