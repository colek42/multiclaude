package main

import (
	_ "embed"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsec2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsssm"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

//go:embed user-data.sh
var userData string

func NewMulticlaudeStack(scope constructs.Construct, id string, props *awscdk.StackProps) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, props)

	// VPC (default)
	vpc := awsec2.Vpc_FromLookup(stack, jsii.String("VPC"), &awsec2.VpcLookupOptions{
		IsDefault: jsii.Bool(true),
	})

	// Security group - egress only (Tailscale handles SSH)
	sg := awsec2.NewSecurityGroup(stack, jsii.String("MulticlaudeSG"), &awsec2.SecurityGroupProps{
		Vpc:              vpc,
		Description:      jsii.String("Multiclaude dev - egress only (SSH via Tailscale)"),
		AllowAllOutbound: jsii.Bool(true),
	})

	// Secrets - values are added manually after deployment
	// Note: Claude uses device auth flow (interactive login), no secret needed
	githubSecret := awssecretsmanager.NewSecret(stack, jsii.String("GithubToken"), &awssecretsmanager.SecretProps{
		SecretName:  jsii.String("multiclaude/github-token"),
		Description: jsii.String("GitHub PAT for multiclaude-agent user"),
	})

	githubSSHKey := awssecretsmanager.NewSecret(stack, jsii.String("GithubSSHKey"), &awssecretsmanager.SecretProps{
		SecretName:  jsii.String("multiclaude/github-ssh-key"),
		Description: jsii.String("SSH private key for multiclaude-agent GitHub user"),
	})

	tailscaleSecret := awssecretsmanager.NewSecret(stack, jsii.String("TailscaleKey"), &awssecretsmanager.SecretProps{
		SecretName:  jsii.String("multiclaude/tailscale-auth-key"),
		Description: jsii.String("Tailscale auth key (reusable, ephemeral)"),
	})

	// IAM role for EC2
	role := awsiam.NewRole(stack, jsii.String("MulticlaudeRole"), &awsiam.RoleProps{
		AssumedBy:   awsiam.NewServicePrincipal(jsii.String("ec2.amazonaws.com"), nil),
		Description: jsii.String("Role for multiclaude EC2 instance"),
		ManagedPolicies: &[]awsiam.IManagedPolicy{
			awsiam.ManagedPolicy_FromAwsManagedPolicyName(jsii.String("AmazonSSMManagedInstanceCore")),
		},
	})

	// Grant secrets read access (nil for versionStages = all versions)
	githubSecret.GrantRead(role, nil)
	githubSSHKey.GrantRead(role, nil)
	tailscaleSecret.GrantRead(role, nil)

	// EC2 instance
	instance := awsec2.NewInstance(stack, jsii.String("MulticlaudeInstance"), &awsec2.InstanceProps{
		Vpc:           vpc,
		InstanceType:  awsec2.InstanceType_Of(awsec2.InstanceClass_T3, awsec2.InstanceSize_MEDIUM),
		MachineImage:  awsec2.MachineImage_LatestAmazonLinux2023(nil),
		SecurityGroup: sg,
		Role:          role,
		InstanceName:  jsii.String("multiclaude-dev"),
		BlockDevices: &[]*awsec2.BlockDevice{
			{
				DeviceName: jsii.String("/dev/xvda"),
				Volume: awsec2.BlockDeviceVolume_Ebs(jsii.Number(50), &awsec2.EbsDeviceOptions{
					VolumeType: awsec2.EbsDeviceVolumeType_GP3,
					Encrypted:  jsii.Bool(true),
				}),
			},
		},
		UserData: awsec2.UserData_Custom(jsii.String(userData)),
	})

	// SSM document for deployments
	awsssm.NewCfnDocument(stack, jsii.String("DeployDocument"), &awsssm.CfnDocumentProps{
		Name:         jsii.String("multiclaude-deploy"),
		DocumentType: jsii.String("Command"),
		Content: map[string]interface{}{
			"schemaVersion": "2.2",
			"description":   "Deploy multiclaude from GitHub",
			"mainSteps": []map[string]interface{}{
				{
					"action": "aws:runShellScript",
					"name":   "deploy",
					"inputs": map[string]interface{}{
						"runCommand": []string{
							"sudo -u dev /home/dev/deploy.sh",
						},
						"timeoutSeconds": "600",
					},
				},
			},
		},
	})

	// GitHub Actions OIDC provider
	oidcProvider := awsiam.NewOpenIdConnectProvider(stack, jsii.String("GitHubOIDC"), &awsiam.OpenIdConnectProviderProps{
		Url: jsii.String("https://token.actions.githubusercontent.com"),
		ClientIds: &[]*string{
			jsii.String("sts.amazonaws.com"),
		},
		Thumbprints: &[]*string{
			// GitHub's OIDC thumbprints
			jsii.String("6938fd4d98bab03faadb97b34396831e3780aea1"),
			jsii.String("1c58a3a8518e8759bf075b76b750d4f2df264fcd"),
		},
	})

	// IAM role for GitHub Actions
	githubActionsRole := awsiam.NewRole(stack, jsii.String("GitHubActionsRole"), &awsiam.RoleProps{
		RoleName:    jsii.String("github-actions-multiclaude"),
		Description: jsii.String("Role for GitHub Actions to deploy multiclaude"),
		AssumedBy: awsiam.NewFederatedPrincipal(
			oidcProvider.OpenIdConnectProviderArn(),
			&map[string]interface{}{
				"StringEquals": map[string]string{
					"token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
				},
				"StringLike": map[string]string{
					// Allow any fork of multiclaude
					"token.actions.githubusercontent.com:sub": "repo:*/multiclaude:*",
				},
			},
			jsii.String("sts:AssumeRoleWithWebIdentity"),
		),
	})

	// Construct instance ARN using Fn.Sub
	instanceArn := awscdk.Fn_Sub(jsii.String("arn:aws:ec2:${AWS::Region}:${AWS::AccountId}:instance/${InstanceId}"), &map[string]*string{
		"InstanceId": instance.InstanceId(),
	})

	// Allow GitHub Actions to send SSM commands
	githubActionsRole.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: &[]*string{
			jsii.String("ssm:SendCommand"),
			jsii.String("ssm:GetCommandInvocation"),
		},
		Resources: &[]*string{
			jsii.String("arn:aws:ssm:*:*:document/multiclaude-deploy"),
			instanceArn,
		},
	}))

	// Allow GitHub Actions to describe instances (for SSM)
	githubActionsRole.AddToPolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Effect: awsiam.Effect_ALLOW,
		Actions: &[]*string{
			jsii.String("ec2:DescribeInstances"),
		},
		Resources: &[]*string{
			jsii.String("*"),
		},
	}))

	// Outputs
	awscdk.NewCfnOutput(stack, jsii.String("InstanceId"), &awscdk.CfnOutputProps{
		Value:       instance.InstanceId(),
		Description: jsii.String("EC2 Instance ID for SSM commands and GitHub Actions"),
	})

	awscdk.NewCfnOutput(stack, jsii.String("GitHubActionsRoleArn"), &awscdk.CfnOutputProps{
		Value:       githubActionsRole.RoleArn(),
		Description: jsii.String("IAM Role ARN for GitHub Actions OIDC"),
	})

	awscdk.NewCfnOutput(stack, jsii.String("GitHubSecretArn"), &awscdk.CfnOutputProps{
		Value:       githubSecret.SecretArn(),
		Description: jsii.String("ARN of GitHub token secret"),
	})

	awscdk.NewCfnOutput(stack, jsii.String("GitHubSSHKeyArn"), &awscdk.CfnOutputProps{
		Value:       githubSSHKey.SecretArn(),
		Description: jsii.String("ARN of GitHub SSH private key secret"),
	})

	awscdk.NewCfnOutput(stack, jsii.String("TailscaleSecretArn"), &awscdk.CfnOutputProps{
		Value:       tailscaleSecret.SecretArn(),
		Description: jsii.String("ARN of Tailscale auth key secret"),
	})

	return stack
}

func main() {
	defer jsii.Close()

	app := awscdk.NewApp(nil)

	NewMulticlaudeStack(app, "MulticlaudeStack", &awscdk.StackProps{
		Env: &awscdk.Environment{
			Account: jsii.String("898769392027"),
			Region:  jsii.String("us-east-1"),
		},
	})

	app.Synth(nil)
}
