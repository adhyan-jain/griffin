#!/usr/bin/env bash
# Griffin AWS EC2 Provisioning Script
# Target: ap-south-1 (Mumbai)
# Instance: t4g.medium (ARM64 Graviton2)
set -euo pipefail

REGION="${GRIFFIN_AWS_REGION:-ap-south-1}"
INSTANCE_TYPE="t4g.medium"
ROLE_NAME="GriffinEC2WorkerRole"
PROFILE_NAME="GriffinEC2WorkerProfile"
SG_NAME="griffin-runner-sg"
TAG_NAME="griffin-cloud-runner"
PROFILE_ARG=""

if [ -n "${AWS_PROFILE:-}" ]; then
    PROFILE_ARG="--profile ${AWS_PROFILE}"
elif aws configure list-profiles | grep -q "griffin-bootstrap"; then
    PROFILE_ARG="--profile griffin-bootstrap"
fi

echo "=== Griffin AWS EC2 Cloud Runner Provisioning ==="
echo "Target Region: ${REGION}"
echo "Instance Type: ${INSTANCE_TYPE}"
echo "AWS Profile:   ${PROFILE_ARG:-default}"

# 1. Resolve latest official Ubuntu 24.04 ARM64 AMI
echo "[1/5] Resolving official Ubuntu 24.04 ARM64 AMI..."
AMI_ID=$(aws ec2 describe-images ${PROFILE_ARG} \
    --region "${REGION}" \
    --owners 099720109477 \
    --filters "Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-arm64-server-*" \
              "Name=state,Values=available" \
    --query "reverse(sort_by(Images, &CreationDate))[0].ImageId" \
    --output text 2>/dev/null || true)

if [ -z "${AMI_ID}" ] || [ "${AMI_ID}" == "None" ]; then
    echo "Notice: Could not dynamically describe images (IAM permission required). Using verified Ubuntu 24.04 ARM64 AMI for ap-south-1: ami-0d8b4e4e9a59cf861"
    AMI_ID="ami-0d8b4e4e9a59cf861"
fi

echo "Selected AMI: ${AMI_ID}"

# 2. Create IAM Role and Instance Profile for SSM Session Manager + S3 Object Storage
echo "[2/5] Configuring IAM Role for SSM Session Manager and S3 Storage..."
if ! aws iam get-role ${PROFILE_ARG} --role-name "${ROLE_NAME}" >/dev/null 2>&1; then
    aws iam create-role ${PROFILE_ARG} \
        --role-name "${ROLE_NAME}" \
        --assume-role-policy-document '{
            "Version": "2012-10-17",
            "Statement": [{
                "Effect": "Allow",
                "Principal": {"Service": "ec2.amazonaws.com"},
                "Action": "sts:AssumeRole"
            }]
        }'
    aws iam attach-role-policy ${PROFILE_ARG} \
        --role-name "${ROLE_NAME}" \
        --policy-arn "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"

    # Attach S3 artifact access
    aws iam put-role-policy ${PROFILE_ARG} \
        --role-name "${ROLE_NAME}" \
        --policy-name "GriffinS3ArtifactAccess" \
        --policy-document '{
            "Version": "2012-10-17",
            "Statement": [
                {
                    "Effect": "Allow",
                    "Action": [
                        "s3:PutObject",
                        "s3:GetObject",
                        "s3:DeleteObject",
                        "s3:HeadObject"
                    ],
                    "Resource": "arn:aws:s3:::griffin-artifacts-*/*"
                },
                {
                    "Effect": "Allow",
                    "Action": [
                        "s3:ListBucket"
                    ],
                    "Resource": "arn:aws:s3:::griffin-artifacts-*"
                }
            ]
        }'
fi

if ! aws iam get-instance-profile ${PROFILE_ARG} --instance-profile-name "${PROFILE_NAME}" >/dev/null 2>&1; then
    aws iam create-instance-profile ${PROFILE_ARG} --instance-profile-name "${PROFILE_NAME}" || true
    aws iam add-role-to-instance-profile ${PROFILE_ARG} \
        --instance-profile-name "${PROFILE_NAME}" \
        --role-name "${ROLE_NAME}" || true
    sleep 10 # Wait for IAM propagation
fi

# 3. Create Security Group (ZERO INBOUND PORTS)
echo "[3/5] Configuring Security Group (Inbound Port 22 CLOSED)..."
VPC_ID=$(aws ec2 describe-vpcs ${PROFILE_ARG} --region "${REGION}" --filters "Name=isDefault,Values=true" --query "Vpcs[0].VpcId" --output text 2>/dev/null || true)

if [ -n "${VPC_ID}" ] && [ "${VPC_ID}" != "None" ]; then
    SG_ID=$(aws ec2 describe-security-groups ${PROFILE_ARG} --region "${REGION}" --filters "Name=group-name,Values=${SG_NAME}" "Name=vpc-id,Values=${VPC_ID}" --query "SecurityGroups[0].GroupId" --output text 2>/dev/null || true)

    if [ "${SG_ID}" == "None" ] || [ -z "${SG_ID}" ]; then
        SG_ID=$(aws ec2 create-security-group ${PROFILE_ARG} \
            --region "${REGION}" \
            --group-name "${SG_NAME}" \
            --description "Griffin Cloud Runner SG - Zero Inbound Ports" \
            --vpc-id "${VPC_ID}" \
            --query "GroupId" --output text 2>/dev/null || true)
    fi
    echo "Security Group ID: ${SG_ID}"
fi

# 4. Launch EC2 Instance with encrypted 30 GiB gp3 root volume
echo "[4/5] Launching EC2 ${INSTANCE_TYPE} instance..."
if ! INSTANCE_ID=$(aws ec2 run-instances ${PROFILE_ARG} \
    --region "${REGION}" \
    --image-id "${AMI_ID}" \
    --instance-type "${INSTANCE_TYPE}" \
    --iam-instance-profile Name="${PROFILE_NAME}" \
    ${SG_ID:+--security-group-ids "${SG_ID}"} \
    --block-device-mappings '[{
        "DeviceName": "/dev/sda1",
        "Ebs": {
            "VolumeSize": 30,
            "VolumeType": "gp3",
            "Encrypted": true,
            "DeleteOnTermination": false
        }
    }]' \
    --user-data file://infra/user-data.sh \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=${TAG_NAME}},{Key=Project,Value=Griffin}]" \
    --query "Instances[0].InstanceId" \
    --output text 2>&1); then
    echo "EC2 run-instances output: ${INSTANCE_ID}"
    echo "NOTE: If UnauthorizedOperation occurred, please attach the EC2 provisioning policy from docs/DEPLOYMENT_RUNBOOK.md to griffin-bootstrap."
    exit 1
fi

echo "Launched Instance ID: ${INSTANCE_ID}"
echo "Waiting for instance to reach 'running' state..."
aws ec2 wait instance-running ${PROFILE_ARG} --region "${REGION}" --instance-ids "${INSTANCE_ID}"

echo "[5/5] Instance is running!"
echo "Connect via SSM Session Manager using:"
echo "aws ssm start-session --target ${INSTANCE_ID} --region ${REGION}"
